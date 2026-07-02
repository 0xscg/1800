package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testStore connects to a throwaway Postgres and applies the migration into a
// fresh schema. Set TEST_DATABASE_URL to run; otherwise it tries the local
// dev container and skips when nothing is reachable.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:test@localhost:55432/longevity_test?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := New(ctx, url)
	if err == nil {
		err = s.Pool.Ping(ctx)
	}
	if err != nil {
		t.Skipf("no test database reachable (%v); set TEST_DATABASE_URL to run view tests", err)
	}
	t.Cleanup(s.Pool.Close)

	if _, err := s.Pool.Exec(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0001_init.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := s.Pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	return s
}

func day(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestUpsertIdempotency(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ { // resend must be harmless
		if err := s.UpsertRawEvent(ctx, "whoop", "sleep", "uuid-1", []byte(`{"v":1}`)); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertDailyMetric(ctx, day(t, "2026-07-01"), "resting_hr", "whoop", 52); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM raw_events`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("raw_events count = %d err=%v, want 1", n, err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM daily_metrics`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("daily_metrics count = %d err=%v, want 1", n, err)
	}

	// Late re-send with a corrected value updates in place.
	if err := s.UpsertDailyMetric(ctx, day(t, "2026-07-01"), "resting_hr", "whoop", 54); err != nil {
		t.Fatal(err)
	}
	var v float64
	if err := s.Pool.QueryRow(ctx, `SELECT value FROM daily_metrics`).Scan(&v); err != nil || v != 54 {
		t.Fatalf("value = %v err=%v, want 54", v, err)
	}
}

func TestMetricPreferredSourcePolicy(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	d := day(t, "2026-07-01")

	// Physiology metric present from both sources → whoop wins.
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.UpsertDailyMetric(ctx, d, "resting_hr", "whoop", 52))
	must(s.UpsertDailyMetric(ctx, d, "resting_hr", "watch", 58))
	// Activity metric present from both sources → watch wins.
	must(s.UpsertDailyMetric(ctx, d, "steps", "whoop", 100))
	must(s.UpsertDailyMetric(ctx, d, "steps", "watch", 10432))

	var src string
	var v float64
	if err := s.Pool.QueryRow(ctx,
		`SELECT source, value FROM metric_preferred WHERE metric='resting_hr' AND day=$1`, d).Scan(&src, &v); err != nil {
		t.Fatal(err)
	}
	if src != "whoop" || v != 52 {
		t.Fatalf("resting_hr preferred = %s/%v, want whoop/52", src, v)
	}
	if err := s.Pool.QueryRow(ctx,
		`SELECT source, value FROM metric_preferred WHERE metric='steps' AND day=$1`, d).Scan(&src, &v); err != nil {
		t.Fatal(err)
	}
	if src != "watch" || v != 10432 {
		t.Fatalf("steps preferred = %s/%v, want watch/10432", src, v)
	}

	// rMSSD and SDNN stay separate rows, never merged.
	must(s.UpsertDailyMetric(ctx, d, "hrv_rmssd_ms", "whoop", 68))
	must(s.UpsertDailyMetric(ctx, d, "hrv_sdnn_ms", "watch", 41))
	var n int
	if err := s.Pool.QueryRow(ctx,
		`SELECT count(*) FROM metric_preferred WHERE day=$1 AND metric LIKE 'hrv_%'`, d).Scan(&n); err != nil || n != 2 {
		t.Fatalf("hrv rows in metric_preferred = %d err=%v, want 2", n, err)
	}
}

func TestBaselineExcludesCurrentDay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// 10 steady days at 50, then a spike to 100 on the last day.
	start := day(t, "2026-06-20")
	for i := 0; i < 10; i++ {
		if err := s.UpsertDailyMetric(ctx, start.AddDate(0, 0, i), "resting_hr", "whoop", 50); err != nil {
			t.Fatal(err)
		}
	}
	spike := start.AddDate(0, 0, 10) // 2026-06-30
	if err := s.UpsertDailyMetric(ctx, spike, "resting_hr", "whoop", 100); err != nil {
		t.Fatal(err)
	}

	var mean, sd, z *float64
	if err := s.Pool.QueryRow(ctx,
		`SELECT mean30, sd30, z FROM metric_scored WHERE metric='resting_hr' AND day=$1`, spike).Scan(&mean, &sd, &z); err != nil {
		t.Fatal(err)
	}
	// If the window included the spike day itself, mean would be > 50.
	if mean == nil || *mean != 50 {
		t.Fatalf("mean30 on spike day = %v, want exactly 50 (current day excluded)", mean)
	}
	// sd of ten identical 50s is 0 → z must be NULL (divide-by-zero guarded).
	if sd == nil || *sd != 0 {
		t.Fatalf("sd30 = %v, want 0", sd)
	}
	if z != nil {
		t.Fatalf("z = %v, want NULL when sd30 = 0", *z)
	}

	// First day has no preceding rows at all → mean30 NULL.
	if err := s.Pool.QueryRow(ctx,
		`SELECT mean30 FROM metric_scored WHERE metric='resting_hr' AND day=$1`, start).Scan(&mean); err != nil {
		t.Fatal(err)
	}
	if mean != nil {
		t.Fatalf("first-day mean30 = %v, want NULL", *mean)
	}
}
