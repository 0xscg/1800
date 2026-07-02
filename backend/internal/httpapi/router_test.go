package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sushan/longevity/internal/config"
	"github.com/sushan/longevity/internal/store"
	"github.com/sushan/longevity/internal/whoop"
)

// testAPI wires a real store (throwaway Postgres schema) into the API.
// Skips when no test database is reachable, same policy as store tests.
func testAPI(t *testing.T) *API {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = "postgres://postgres:test@localhost:55432/longevity_test?sslmode=disable"
	}
	// Own schema so these tests don't race the store package's schema reset
	// when packages run in parallel.
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	url += sep + "options=" + neturl.QueryEscape("-csearch_path=httpapi_test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := store.New(ctx, url)
	if err == nil {
		err = s.Pool.Ping(ctx)
	}
	if err != nil {
		t.Skipf("no test database reachable (%v); set TEST_DATABASE_URL to run API tests", err)
	}
	t.Cleanup(s.Pool.Close)

	if _, err := s.Pool.Exec(ctx, `DROP SCHEMA IF EXISTS httpapi_test CASCADE; CREATE SCHEMA httpapi_test`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	sql, err := os.ReadFile(filepath.Join("..", "..", "migrations", "0001_init.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := s.Pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}

	cfg := config.Config{DeviceIngestToken: "test-token"}
	return &API{Cfg: cfg, Store: s, Whoop: whoop.New("id", "secret", "http://cb", s)}
}

func postSamples(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/ingest/samples", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// Fix 2: an invalid batch must 400 and persist NOTHING — no raw_events row,
// no partial daily_metrics from the valid leading samples.
func TestIngestInvalidBatchPersistsNothing(t *testing.T) {
	a := testAPI(t)
	h := a.Router()
	ctx := context.Background()

	w := postSamples(t, h, `{"batch_id":"b1","samples":[
		{"day":"2026-07-01","metric":"steps","value":100},
		{"day":"2026-07-01","metric":"blood_type","value":1}]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var n int
	if err := a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM raw_events`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("raw_events count = %d err=%v, want 0", n, err)
	}
	if err := a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM daily_metrics`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("daily_metrics count = %d err=%v, want 0", n, err)
	}

	// A valid batch still lands.
	w = postSamples(t, h, `{"batch_id":"b2","samples":[{"day":"2026-07-01","metric":"steps","value":100}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("valid batch status = %d body=%s, want 200", w.Code, w.Body)
	}
	if err := a.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM raw_events`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("raw_events after valid batch = %d err=%v, want 1", n, err)
	}
}

// Fix 3: sparkline is exactly the last 14 CALENDAR days, oldest first, zero-filled.
func TestTodaySparklineIs14CalendarDays(t *testing.T) {
	a := testAPI(t)
	ctx := context.Background()

	// Seed 40 days of history ending 5 days ago, so the trailing 14-day window
	// contains 9 defined days followed by a 5-day gap (must be zero-filled).
	for i := 0; i < 40; i++ {
		day := fmt.Sprintf("CURRENT_DATE - %d", 44-i)
		if _, err := a.Store.Pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO daily_metrics (day, metric, source, value) VALUES (%s, 'resting_hr', 'whoop', $1)`, day),
			50+float64(i%7)); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest("GET", "/v1/dashboard/today", nil)
	w := httptest.NewRecorder()
	a.Router().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var out []MetricToday
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("metrics = %d, want 1", len(out))
	}
	if len(out[0].Spark) != 14 {
		t.Fatalf("spark length = %d, want exactly 14 calendar days", len(out[0].Spark))
	}
	// The last 5 calendar days have no data → trailing zeros.
	for i := 9; i < 14; i++ {
		if out[0].Spark[i] != 0 {
			t.Fatalf("spark[%d] = %v, want 0 for a day with no score", i, out[0].Spark[i])
		}
	}
}

// Fix 8: days=N returns at most N calendar days.
func TestSeriesDaysBoundary(t *testing.T) {
	a := testAPI(t)
	ctx := context.Background()

	// 10 consecutive days ending today.
	for i := 0; i < 10; i++ {
		if _, err := a.Store.Pool.Exec(ctx, fmt.Sprintf(
			`INSERT INTO daily_metrics (day, metric, source, value) VALUES (CURRENT_DATE - %d, 'steps', 'watch', $1)`, i),
			float64(1000+i)); err != nil {
			t.Fatal(err)
		}
	}

	get := func(days int) []SeriesPoint {
		req := httptest.NewRequest("GET", fmt.Sprintf("/v1/metrics/steps?days=%d", days), nil)
		w := httptest.NewRecorder()
		a.Router().ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("status = %d body=%s", w.Code, w.Body)
		}
		var out []SeriesPoint
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := get(7); len(got) != 7 {
		t.Fatalf("days=7 returned %d points, want 7", len(got))
	}
	if got := get(1); len(got) != 1 {
		t.Fatalf("days=1 returned %d points, want 1", len(got))
	}
	if got := get(30); len(got) != 10 { // only 10 days exist
		t.Fatalf("days=30 returned %d points, want 10", len(got))
	}
}

// Fix 4: transient failures inside the background handler are retried with
// backoff; the event lands once the dependency recovers.
func TestWhoopEventRetriesTransientFailure(t *testing.T) {
	a := testAPI(t)

	var hits atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 2 { // fail the first two attempts
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"id":"uuid-1","end":"2026-07-02T06:45:00Z","timezone_offset":"+01:00","nap":false,
			"score":{"stage_summary":{"total_light_sleep_time_milli":3600000}}}`))
	}))
	defer api.Close()

	a.Whoop.APIBase = api.URL
	a.Whoop.SetTokenSource(func(ctx context.Context, force bool) (string, error) { return "t", nil })

	oldWaits := whoopRetryWaits
	whoopRetryWaits = []time.Duration{time.Millisecond, time.Millisecond}
	defer func() { whoopRetryWaits = oldWaits }()

	a.processWhoopEvent(whoop.WebhookEvent{Type: "sleep.updated", ID: json.RawMessage(`"uuid-1"`)})

	if got := hits.Load(); got != 3 {
		t.Fatalf("whoop API hit %d times, want 3 (two failures + one success)", got)
	}
	var n int
	if err := a.Store.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM raw_events WHERE provider='whoop' AND kind='sleep'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("raw_events sleep count = %d err=%v, want 1", n, err)
	}
}
