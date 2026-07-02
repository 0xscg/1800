---
name: baseline-stats
description: The statistics engine of this project — how personal baselines, z-scores, and source-of-truth views work in Postgres, and the exact end-to-end checklist for adding a new metric. Use this skill whenever adding or renaming a metric, changing baseline windows or deviation thresholds, touching migrations or the metric_preferred/metric_scored views, writing queries against daily_metrics, or when the user asks about z-scores, baselines, "vs your norm", sparklines, or why a number looks wrong on the dashboard.
---

# Baseline statistics

## How scoring works (all in SQL, never in app code)

`daily_metrics(day, metric, source, value)` → view `metric_preferred` picks ONE source
per (day, metric) via a CASE priority (Whoop wins physiology; watch wins activity) →
view `metric_scored` adds, per metric ordered by day:

```sql
AVG(value)         OVER w AS mean30,
STDDEV_SAMP(value) OVER w AS sd30,
(value - mean30) / NULLIF(sd30, 0) AS z
WINDOW w AS (PARTITION BY metric ORDER BY day ROWS BETWEEN 30 PRECEDING AND 1 PRECEDING)
```

Non-negotiables:
- Window EXCLUDES today (`... AND 1 PRECEDING`) — today can't dilute its own norm.
- The window is 30 *rows* (observed days), not 30 calendar days. Gaps stretch the window.
  Acceptable for a personal tool; if changing to calendar days, use RANGE with an interval
  and discuss first.
- z is NULL until ~2 prior observations exist (sd undefined); the UI shows "building baseline…".
- Never compute means/z in Go/TS/Kotlin. If a new stat is needed, add a view.

## Deviation semantics (shared by web + Compose — keep in sync)

- |z| < 0.75 → "on baseline" (neutral). ≥ 0.75 → colored by direction.
- Direction map (`higherIsBetter`): true = hrv_rmssd_ms, sleep_min, sleep_efficiency,
  recovery_score, steps, active_kcal, vo2max, hrv_sdnn_ms; false = resting_hr;
  null = respiratory_rate (any deviation = attention).
- These live in `web/src/api/types.ts` (METRICS) and `DashboardScreen.kt` (HIGHER_IS_BETTER).
  Change both or the platforms disagree.

## Checklist: adding a new metric (do ALL steps)

1. Name it `snake_case` with the unit suffix when ambiguous (e.g. `hrv_rmssd_ms`).
2. Decide day attribution (overnight metrics → wake day) and the winning source; add the
   metric to the correct CASE arm in `metric_preferred` (new migration, not an edit of 0001).
3. Emit it in `backend/internal/normalize` (Whoop) and/or the device shim path
   (`DeviceSample` enum in contracts/openapi.yaml + Health Connect reader).
4. Add to `web/src/api/types.ts` METRICS (label, unit, higherIsBetter, decimals)
   and to `mock.ts` BASES so the mock dashboard shows it.
5. Add to Compose maps (LABELS, HIGHER_IS_BETTER) in `DashboardScreen.kt`.
6. If it deserves a trend panel, add to HERO_TRENDS in `web/src/App.tsx`.
7. Sanity-check: `SELECT * FROM metric_scored WHERE metric='<new>' ORDER BY day DESC LIMIT 5;`

## Debugging "the number looks wrong"

Check in this order: raw payload in `raw_events` → normalized row in `daily_metrics`
(right day? right source?) → `metric_preferred` (did the wrong source win?) →
`metric_scored` (is sd tiny, inflating z?). 90% of issues are day attribution or
a duplicate source fighting the priority CASE.
