---
name: health-connect-sync
description: The Android device shim — reading Health Connect, aggregating on-device, and posting idempotent batches to the backend. Use this skill whenever touching android/app code, adding a new Health Connect record type, debugging missing watch/phone data, changing sync scheduling, or when the user mentions Health Connect, HealthKit parity, WorkManager, steps/sleep not appearing, or Android permissions.
---

# Health Connect sync shim

## Division of labour (do not blur it)

The PHONE owns device quirks: reading records, deduping overlapping sources, and
pre-aggregating to ONE value per (day, metric). The SERVER stays dumb: it accepts
`{day, metric, value}` samples and upserts. Never send raw samples to the backend;
never aggregate on the server.

## Aggregation rules (HealthConnectReader.kt)

- steps, active_kcal: use the AGGREGATE API (`AggregateRequest`) — it dedupes
  overlapping sources (phone + watch both counting steps) for free. Never sum raw
  StepsRecords yourself; you will double-count.
- hrv_rmssd_ms: mean of the day's HeartRateVariabilityRmssd readings.
- resting_hr: MINIMUM of the day's RestingHeartRate records.
- sleep_min: sum of SleepSession durations in the day window; sleep belongs to the
  window it ENDS in (wake day) — same rule as the Whoop side.
- New record types: add the permission to BOTH the manifest and
  `HealthConnectReader.permissions`, extend the reader, add the metric to the
  `DeviceSample` enum in contracts/openapi.yaml, then follow the baseline-stats
  new-metric checklist.

## Sync mechanics

- WorkManager unique periodic work ("health-sync", 6h, KEEP policy). Android throttles
  background work — treat delivery as "a few times a day", never assume real-time.
- Each run re-reads the last 7 days and posts them all. This is deliberate: Health
  Connect backfills late (watch syncs, app imports), and server upserts make
  re-sending free. Do not "optimize" to only-today without replacing it with
  changes-token incremental reads.
- Batch POST: `Authorization: Bearer <INGEST_TOKEN>` to `/v1/ingest/samples` with a
  UUID batch_id. Failure → Result.retry() (WorkManager backoff handles it).
- Emulator reaches the host backend at `10.0.2.2:8080`.

## Permission flow

Request via `PermissionController.createRequestPermissionResultContract()` (already in
MainActivity). Health Connect permissions are NOT runtime permissions — don't use the
standard ActivityCompat path. The manifest also needs the
`ACTION_SHOW_PERMISSIONS_RATIONALE` intent filter (present) or the request silently
no-ops on some devices. If data is missing, check: permission granted in the Health
Connect app → records visible in Health Connect's own UI → then debug the reader.

## Privacy

Health values never go to logs, crash reports, or analytics. Log counts and day ranges
only (e.g. "posted 23 samples for 7 days"), never values.
