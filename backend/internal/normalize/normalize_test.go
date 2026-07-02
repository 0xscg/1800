package normalize

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// fakeWriter records upserts keyed like the daily_metrics unique constraint,
// so replaying input twice must leave the map unchanged (idempotency).
type fakeWriter struct {
	rows  map[string]float64
	calls int
}

func newFake() *fakeWriter { return &fakeWriter{rows: map[string]float64{}} }

func (f *fakeWriter) UpsertDailyMetric(_ context.Context, day time.Time, metric, source string, value float64) error {
	f.calls++
	f.rows[fmt.Sprintf("%s|%s|%s", day.Format("2006-01-02"), metric, source)] = value
	return nil
}

const sleepPayload = `{
	"id": "sleep-uuid-1",
	"end": "2026-07-02T06:45:00Z",
	"nap": false,
	"score": {
		"stage_summary": {
			"total_in_bed_time_milli": 30600000,
			"total_awake_time_milli": 1800000,
			"total_light_sleep_time_milli": 14400000,
			"total_slow_wave_sleep_time_milli": 7200000,
			"total_rem_sleep_time_milli": 7200000
		},
		"sleep_efficiency_percentage": 94.1,
		"respiratory_rate": 15.2
	}
}`

func TestSleepKeyedToWakeDay(t *testing.T) {
	f := newFake()
	if err := Sleep(context.Background(), f, []byte(sleepPayload)); err != nil {
		t.Fatal(err)
	}
	// end = 2026-07-02T06:45Z → sleep belongs to wake day 2026-07-02.
	if got, ok := f.rows["2026-07-02|sleep_min|whoop"]; !ok || got != 480 { // 28800000ms asleep = 480 min
		t.Fatalf("sleep_min on wake day: got %v ok=%v, want 480", got, ok)
	}
	if got := f.rows["2026-07-02|sleep_efficiency|whoop"]; got != 94.1 {
		t.Fatalf("sleep_efficiency = %v, want 94.1", got)
	}
	if got := f.rows["2026-07-02|respiratory_rate|whoop"]; got != 15.2 {
		t.Fatalf("respiratory_rate = %v, want 15.2", got)
	}
}

func TestSleepNapIgnored(t *testing.T) {
	f := newFake()
	nap := `{"id":"n1","end":"2026-07-02T15:00:00Z","nap":true,"score":{"stage_summary":{"total_light_sleep_time_milli":3600000}}}`
	if err := Sleep(context.Background(), f, []byte(nap)); err != nil {
		t.Fatal(err)
	}
	if len(f.rows) != 0 {
		t.Fatalf("nap wrote %d rows, want 0", len(f.rows))
	}
}

// BST regression: waking at 00:30 local (23:30 UTC the previous day) must
// attribute the sleep to the LOCAL wake day, not the UTC one.
func TestSleepWakeDayUsesTimezoneOffset(t *testing.T) {
	f := newFake()
	payload := `{
		"id": "sleep-bst",
		"end": "2026-07-01T23:30:00Z",
		"timezone_offset": "+01:00",
		"nap": false,
		"score": {"stage_summary": {"total_light_sleep_time_milli": 3600000}}
	}`
	if err := Sleep(context.Background(), f, []byte(payload)); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.rows["2026-07-02|sleep_min|whoop"]; !ok {
		t.Fatalf("BST 00:30 wake should land on local day 2026-07-02, got %v", f.rows)
	}
	if _, ok := f.rows["2026-07-01|sleep_min|whoop"]; ok {
		t.Fatal("sleep attributed to UTC day 2026-07-01 instead of local wake day")
	}

	// Negative offsets: 20:00 UTC at -05:00 is 15:00 local, same calendar day.
	d, err := SleepWakeDay([]byte(`{"end":"2026-07-01T02:00:00Z","timezone_offset":"-05:00"}`))
	if err != nil || d.Format("2006-01-02") != "2026-06-30" {
		t.Fatalf("-05:00 offset: day=%v err=%v, want 2026-06-30", d, err)
	}
	// Missing/garbled offset falls back to UTC truncation.
	d, err = SleepWakeDay([]byte(`{"end":"2026-07-01T23:30:00Z","timezone_offset":"nonsense"}`))
	if err != nil || d.Format("2006-01-02") != "2026-07-01" {
		t.Fatalf("bad offset fallback: day=%v err=%v, want 2026-07-01", d, err)
	}
}

// Recovery must land on the SAME wake day as its associated sleep, even when
// its created_at timestamp falls on a different UTC day.
func TestRecoveryKeyedToSleepWakeDay(t *testing.T) {
	f := newFake()
	sleep := `{"id":"s1","end":"2026-07-01T23:30:00Z","timezone_offset":"+01:00","nap":false,
		"score":{"stage_summary":{"total_light_sleep_time_milli":3600000}}}`
	if err := Sleep(context.Background(), f, []byte(sleep)); err != nil {
		t.Fatal(err)
	}
	wakeDay, err := SleepWakeDay([]byte(sleep))
	if err != nil {
		t.Fatal(err)
	}
	recovery := `{"sleep_id":"s1","created_at":"2026-07-01T23:45:00Z","score":{"recovery_score":71}}`
	if err := Recovery(context.Background(), f, []byte(recovery), wakeDay); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.rows["2026-07-02|recovery_score|whoop"]; !ok {
		t.Fatalf("recovery not on sleep's wake day 2026-07-02: %v", f.rows)
	}
	if _, ok := f.rows["2026-07-01|recovery_score|whoop"]; ok {
		t.Fatal("recovery keyed to created_at UTC day instead of sleep wake day")
	}
}

func TestRecoveryFallsBackToCreatedAt(t *testing.T) {
	f := newFake()
	recovery := `{"sleep_id":"s1","created_at":"2026-07-02T07:00:00Z","score":{"recovery_score":71}}`
	if err := Recovery(context.Background(), f, []byte(recovery), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.rows["2026-07-02|recovery_score|whoop"]; !ok {
		t.Fatalf("fallback day wrong: %v", f.rows)
	}
}

func TestRecoveryMapsToRmssdNeverSdnn(t *testing.T) {
	f := newFake()
	payload := `{
		"sleep_id": "sleep-uuid-1",
		"created_at": "2026-07-02T07:00:00Z",
		"score": {"recovery_score": 71, "resting_heart_rate": 52, "hrv_rmssd_milli": 68.5}
	}`
	if err := Recovery(context.Background(), f, []byte(payload), time.Time{}); err != nil {
		t.Fatal(err)
	}
	if got := f.rows["2026-07-02|hrv_rmssd_ms|whoop"]; got != 68.5 {
		t.Fatalf("hrv_rmssd_ms = %v, want 68.5", got)
	}
	for k := range f.rows {
		if k == "2026-07-02|hrv_sdnn_ms|whoop" {
			t.Fatal("whoop recovery must never write hrv_sdnn_ms")
		}
	}
	if got := f.rows["2026-07-02|resting_hr|whoop"]; got != 52 {
		t.Fatalf("resting_hr = %v, want 52", got)
	}
	if got := f.rows["2026-07-02|recovery_score|whoop"]; got != 71 {
		t.Fatalf("recovery_score = %v, want 71", got)
	}
}

func TestDeviceSamples(t *testing.T) {
	f := newFake()
	samples := []DeviceSample{
		{Day: "2026-07-01", Metric: "steps", Value: 10432},
		{Day: "2026-07-01", Metric: "hrv_sdnn_ms", Value: 41.2},
	}
	if err := Device(context.Background(), f, samples); err != nil {
		t.Fatal(err)
	}
	if f.rows["2026-07-01|steps|watch"] != 10432 {
		t.Fatalf("steps = %v", f.rows["2026-07-01|steps|watch"])
	}
	if f.rows["2026-07-01|hrv_sdnn_ms|watch"] != 41.2 {
		t.Fatalf("hrv_sdnn_ms = %v", f.rows["2026-07-01|hrv_sdnn_ms|watch"])
	}
}

func TestDeviceRejectsBadInput(t *testing.T) {
	f := newFake()
	if err := Device(context.Background(), f, []DeviceSample{{Day: "02/07/2026", Metric: "steps", Value: 1}}); !errors.Is(err, ErrBadSample) {
		t.Fatalf("bad day: err = %v, want ErrBadSample", err)
	}
	if err := Device(context.Background(), f, []DeviceSample{{Day: "2026-07-01", Metric: "blood_type", Value: 1}}); !errors.Is(err, ErrBadSample) {
		t.Fatalf("unknown metric: err = %v, want ErrBadSample", err)
	}
	// A valid sample followed by an invalid one must write NOTHING (no partials).
	batch := []DeviceSample{
		{Day: "2026-07-01", Metric: "steps", Value: 100},
		{Day: "2026-07-01", Metric: "blood_type", Value: 1},
	}
	if err := ValidateDeviceSamples(batch); !errors.Is(err, ErrBadSample) {
		t.Fatalf("ValidateDeviceSamples: err = %v, want ErrBadSample", err)
	}
	if err := Device(context.Background(), f, batch); !errors.Is(err, ErrBadSample) {
		t.Fatalf("mixed batch: err = %v, want ErrBadSample", err)
	}
	if len(f.rows) != 0 || f.calls != 0 {
		t.Fatalf("invalid batch wrote %d rows (%d calls), want 0", len(f.rows), f.calls)
	}
	if err := ValidateDeviceSamples([]DeviceSample{{Day: "2026-07-01", Metric: "steps", Value: 1}}); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
}

// Re-normalizing the same payloads must converge to identical state.
func TestIdempotentRenormalization(t *testing.T) {
	f := newFake()
	run := func() {
		if err := Sleep(context.Background(), f, []byte(sleepPayload)); err != nil {
			t.Fatal(err)
		}
		if err := Device(context.Background(), f, []DeviceSample{{Day: "2026-07-02", Metric: "steps", Value: 9000}}); err != nil {
			t.Fatal(err)
		}
	}
	run()
	first := fmt.Sprint(f.rows)
	run()
	if fmt.Sprint(f.rows) != first {
		t.Fatalf("re-normalization changed state:\nfirst: %s\nsecond: %v", first, f.rows)
	}
	if len(f.rows) != 4 {
		t.Fatalf("row count = %d, want 4", len(f.rows))
	}
}
