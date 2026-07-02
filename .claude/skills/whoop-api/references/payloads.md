# WHOOP v2 payload shapes used by internal/normalize

Only fields we consume. Unknown fields are ignored (json.Unmarshal is lenient).

## Sleep (v2 /activity/sleep/{uuid})
```json
{
  "id": "uuid",
  "end": "2026-07-02T06:41:00Z",
  "nap": false,
  "score": {
    "stage_summary": {
      "total_in_bed_time_milli": 0,
      "total_awake_time_milli": 0,
      "total_light_sleep_time_milli": 0,
      "total_slow_wave_sleep_time_milli": 0,
      "total_rem_sleep_time_milli": 0
    },
    "sleep_performance_percentage": 91.0,
    "sleep_efficiency_percentage": 93.5,
    "respiratory_rate": 14.6
  }
}
```
Rules: naps are skipped; `sleep_min` = (light + slow_wave + rem) / 60000; day = end date (wake day).

## Recovery (v2, keyed to sleep UUID)
```json
{
  "sleep_id": "uuid",
  "created_at": "2026-07-02T06:45:00Z",
  "score": {
    "recovery_score": 68.0,
    "resting_heart_rate": 52.0,
    "hrv_rmssd_milli": 61.8
  }
}
```
Rules: day = created_at date; hrv_rmssd_milli maps to metric `hrv_rmssd_ms`.

## Collection envelope (paginated GETs)
```json
{ "records": [ ... ], "next_token": "opaque-or-absent" }
```
