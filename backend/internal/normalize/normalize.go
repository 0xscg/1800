// Package normalize maps raw provider payloads into daily_metrics rows.
// Re-runnable: everything is upserted, so replaying raw_events rebuilds the table.
package normalize

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrBadSample marks client-side validation failures (vs. storage errors).
var ErrBadSample = errors.New("bad sample")

// MetricWriter is the slice of the store that normalize needs.
// *store.Store satisfies it; tests use an in-memory fake.
type MetricWriter interface {
	UpsertDailyMetric(ctx context.Context, day time.Time, metric, source string, value float64) error
}

// WhoopSleep is the subset of the v2 sleep record we care about.
type WhoopSleep struct {
	ID             string    `json:"id"`
	End            time.Time `json:"end"`
	TimezoneOffset string    `json:"timezone_offset"` // e.g. "+01:00", "-05:00"
	Score          struct {
		StageSummary struct {
			TotalInBedMilli      int64 `json:"total_in_bed_time_milli"`
			TotalAwakeMilli      int64 `json:"total_awake_time_milli"`
			TotalLightMilli      int64 `json:"total_light_sleep_time_milli"`
			TotalSlowWaveMilli   int64 `json:"total_slow_wave_sleep_time_milli"`
			TotalREMMilli        int64 `json:"total_rem_sleep_time_milli"`
		} `json:"stage_summary"`
		SleepPerformancePct *float64 `json:"sleep_performance_percentage"`
		SleepEfficiencyPct  *float64 `json:"sleep_efficiency_percentage"`
		RespiratoryRate     *float64 `json:"respiratory_rate"`
	} `json:"score"`
	Nap bool `json:"nap"`
}

// Day attribution: the sleep belongs to the LOCAL day you woke up.
// WHOOP sends end in UTC plus a timezone_offset like "+01:00"; a 00:30 BST wake
// is 23:30 UTC the previous day and must still land on the local wake day.
func (s WhoopSleep) Day() time.Time {
	end := s.End
	if off, ok := parseTZOffset(s.TimezoneOffset); ok {
		end = end.In(time.FixedZone("whoop", off))
	}
	return time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
}

// parseTZOffset parses "+hh:mm" / "-hh:mm" into seconds east of UTC.
func parseTZOffset(s string) (int, bool) {
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
		return 0, false
	}
	var h, m int
	if _, err := fmt.Sscanf(s[1:], "%02d:%02d", &h, &m); err != nil || h > 23 || m > 59 {
		return 0, false
	}
	sec := h*3600 + m*60
	if s[0] == '-' {
		sec = -sec
	}
	return sec, true
}

// SleepWakeDay extracts the local wake day from a raw sleep payload,
// for callers that need to key an associated record (recovery) to it.
func SleepWakeDay(payload []byte) (time.Time, error) {
	var s WhoopSleep
	if err := json.Unmarshal(payload, &s); err != nil {
		return time.Time{}, err
	}
	return s.Day(), nil
}

func Sleep(ctx context.Context, st MetricWriter, payload []byte) error {
	var s WhoopSleep
	if err := json.Unmarshal(payload, &s); err != nil {
		return err
	}
	if s.Nap {
		return nil // naps don't overwrite the night's numbers
	}
	day := s.Day()
	asleep := s.Score.StageSummary.TotalLightMilli + s.Score.StageSummary.TotalSlowWaveMilli + s.Score.StageSummary.TotalREMMilli
	if asleep > 0 {
		if err := st.UpsertDailyMetric(ctx, day, "sleep_min", "whoop", float64(asleep)/60000); err != nil {
			return err
		}
	}
	if s.Score.SleepEfficiencyPct != nil {
		if err := st.UpsertDailyMetric(ctx, day, "sleep_efficiency", "whoop", *s.Score.SleepEfficiencyPct); err != nil {
			return err
		}
	}
	if s.Score.RespiratoryRate != nil {
		if err := st.UpsertDailyMetric(ctx, day, "respiratory_rate", "whoop", *s.Score.RespiratoryRate); err != nil {
			return err
		}
	}
	return nil
}

// WhoopRecovery is the subset of the v2 recovery record we care about.
// Recovery is generated after a sleep ends — it is keyed to the sleep, not the calendar.
type WhoopRecovery struct {
	SleepID   string    `json:"sleep_id"`
	CreatedAt time.Time `json:"created_at"`
	Score     struct {
		RecoveryScore    *float64 `json:"recovery_score"`
		RestingHeartRate *float64 `json:"resting_heart_rate"`
		HRVRmssdMilli    *float64 `json:"hrv_rmssd_milli"`
	} `json:"score"`
}

// Recovery upserts recovery metrics keyed to wakeDay — the LOCAL wake day of
// the associated sleep (recovery belongs to the same day as its sleep). Pass
// the zero time when the sleep isn't available; created_at is the fallback.
func Recovery(ctx context.Context, st MetricWriter, payload []byte, wakeDay time.Time) error {
	var r WhoopRecovery
	if err := json.Unmarshal(payload, &r); err != nil {
		return err
	}
	day := wakeDay
	if day.IsZero() {
		day = time.Date(r.CreatedAt.Year(), r.CreatedAt.Month(), r.CreatedAt.Day(), 0, 0, 0, 0, time.UTC)
	}
	put := func(metric string, v *float64) error {
		if v == nil {
			return nil
		}
		return st.UpsertDailyMetric(ctx, day, metric, "whoop", *v)
	}
	if err := put("recovery_score", r.Score.RecoveryScore); err != nil {
		return err
	}
	if err := put("resting_hr", r.Score.RestingHeartRate); err != nil {
		return err
	}
	return put("hrv_rmssd_ms", r.Score.HRVRmssdMilli)
}

// DeviceSample is one aggregate from the phone shim (HealthKit / Health Connect).
// The shim pre-aggregates to daily values; keep the server dumb.
type DeviceSample struct {
	Day    string  `json:"day"`    // YYYY-MM-DD
	Metric string  `json:"metric"` // see deviceMetrics; matches the OpenAPI enum
	Value  float64 `json:"value"`
}

// deviceMetrics mirrors the DeviceSample.metric enum in contracts/openapi.yaml.
// Note hrv_sdnn_ms and hrv_rmssd_ms are distinct statistics and stay distinct here.
var deviceMetrics = map[string]bool{
	"steps": true, "active_kcal": true, "vo2max": true,
	"hrv_sdnn_ms": true, "hrv_rmssd_ms": true, "resting_hr": true, "sleep_min": true,
}

// ValidateDeviceSamples checks the whole batch (day format + metric enum)
// without writing anything. Callers validate BEFORE persisting so an invalid
// batch leaves no trace (no raw_events row, no partial daily_metrics).
func ValidateDeviceSamples(samples []DeviceSample) error {
	for i, s := range samples {
		if _, err := time.Parse("2006-01-02", s.Day); err != nil {
			return fmt.Errorf("%w %d: bad day %q", ErrBadSample, i, s.Day)
		}
		if !deviceMetrics[s.Metric] {
			return fmt.Errorf("%w %d: unknown metric %q", ErrBadSample, i, s.Metric)
		}
	}
	return nil
}

// Device upserts a batch of shim samples. The batch must already be valid
// (ValidateDeviceSamples); it is re-checked here so replays stay safe.
func Device(ctx context.Context, st MetricWriter, samples []DeviceSample) error {
	if err := ValidateDeviceSamples(samples); err != nil {
		return err
	}
	for i, s := range samples {
		day, _ := time.Parse("2006-01-02", s.Day)
		if err := st.UpsertDailyMetric(ctx, day, s.Metric, "watch", s.Value); err != nil {
			return fmt.Errorf("sample %d: %w", i, err)
		}
	}
	return nil
}
