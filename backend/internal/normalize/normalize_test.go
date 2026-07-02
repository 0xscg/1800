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

func TestRecoveryMapsToRmssdNeverSdnn(t *testing.T) {
	f := newFake()
	payload := `{
		"sleep_id": "sleep-uuid-1",
		"created_at": "2026-07-02T07:00:00Z",
		"score": {"recovery_score": 71, "resting_heart_rate": 52, "hrv_rmssd_milli": 68.5}
	}`
	if err := Recovery(context.Background(), f, []byte(payload)); err != nil {
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
