# Baseline — personal longevity dashboard (you vs. you)

Every metric is scored against the user's own trailing 30-day baseline (z-scores),
never population norms. That sentence is the product. Protect it.

## Architecture

```
contracts/openapi.yaml   Source of truth for the API. Change here FIRST, then implement.
backend/                 Go 1.22 + chi + pgx. Ingest → normalize → serve. Stats live in SQL views.
web/                     React 18 + TS + Vite + Recharts. Dark instrument UI. Mock fallback in src/api/mock.ts.
android/                 Kotlin + Compose + Health Connect + WorkManager. Dashboard AND device shim.
```

Data flow: Whoop webhook / device POST → `raw_events` (immutable) → `daily_metrics`
(normalized, upserted) → `metric_preferred` (source policy) → `metric_scored` (baselines) → API.

## Commands

```bash
# backend
cd backend && go run ./cmd/api          # serves :8080
psql longevity < migrations/0001_init.sql
go test ./...

# web
cd web && npm run dev                    # :5173, /v1 proxied to :8080; works on mock data alone
npm run build                            # includes tsc type-check — run before considering web work done

# android: open android/ in Android Studio; emulator reaches host via 10.0.2.2:8080
```

## Invariants — do not violate without explicit discussion

1. `raw_events` is append/upsert only. NEVER delete. `daily_metrics` must always be
   rebuildable by replaying raw payloads through `internal/normalize`.
2. Baseline windows EXCLUDE the current day (`ROWS BETWEEN 30 PRECEDING AND 1 PRECEDING`).
   Today's anomaly must not dilute the norm it is judged against.
3. Source-of-truth policy (Whoop wins physiology, watch wins activity) lives ONLY in the
   `metric_preferred` view. Never encode source preference in Go, TS, or Kotlin.
4. Whoop rMSSD (`hrv_rmssd_ms`) and Apple/HC SDNN (`hrv_sdnn_ms`) are different statistics.
   Separate metrics, never merged, never on the same chart line.
5. All ingestion is idempotent: upserts keyed on (provider, kind, external_id) and
   (day, metric, source). Late-arriving and re-sent data must be harmless.
6. Whoop refresh tokens ROTATE. Token refresh must stay inside the row-locked
   transaction in `internal/whoop` (see whoop-api skill before touching it).
7. Webhooks: verify HMAC, ack 200 fast, process in background. Slow acks cause retries.

## Conventions

- Go: stdlib + chi + pgx only. No ORM. SQL lives next to its handler or in views.
- New metric = schema/view + normalize + web types.ts + Compose maps (see baseline-stats skill for the checklist).
- Web/Compose visuals must use the shared tokens (see design-system skill). No new colors.
- Health data is UK GDPR special category. No third-party analytics, no logging of values.
- Dates: metrics are keyed by local calendar day; sleep belongs to the WAKE day.

## Verify-before-trust (unconfirmed against live docs; check on first touch)

- Whoop v2 base path `/developer/v2/...` and the recovery-by-sleep-UUID route.
- Whoop webhook header names: `X-WHOOP-Signature`, `X-WHOOP-Signature-Timestamp`.
- Health Connect record class names vs. the resolved connect-client version.
