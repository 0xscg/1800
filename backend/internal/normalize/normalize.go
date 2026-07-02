// Package normalize maps raw provider payloads into daily_metrics rows.
// Re-runnable: everything is upserted, so replaying raw_events rebuilds the table.
package normalize

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sushan/longevity/internal/store"
)

// WhoopSleep is the subset of the v2 sleep record we care about.
type WhoopSleep struct {
	ID    string    `json:"id"`
	End   time.Time `json:"end"`
	Score struct {
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

// Day attribution: the sleep belongs to the day you woke up.
func (s WhoopSleep) Day() time.Time {
	return time.Date(s.End.Year(), s.End.Month(), s.End.Day(), 0, 0, 0, 0, time.UTC)
}

func Sleep(ctx context.Context, st *store.Store, payload []byte) error {
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

func Recovery(ctx context.Context, st *store.Store, payload []byte) error {
	var r WhoopRecovery
	if err := json.Unmarshal(payload, &r); err != nil {
		return err
	}
	day := time.Date(r.CreatedAt.Year(), r.CreatedAt.Month(), r.CreatedAt.Day(), 0, 0, 0, 0, time.UTC)
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
	Metric string  `json:"metric"` // steps | active_kcal | vo2max | hrv_sdnn_ms | resting_hr
	Value  float64 `json:"value"`
}

func Device(ctx context.Context, st *store.Store, samples []DeviceSample) error {
	for _, s := range samples {
		day, err := time.Parse("2006-01-02", s.Day)
		if err != nil {
			continue
		}
		if err := st.UpsertDailyMetric(ctx, day, s.Metric, "watch", s.Value); err != nil {
			return err
		}
	}
	return nil
}
