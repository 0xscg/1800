# Baseline — a thin longevity dashboard (you vs. you)

Every metric is scored against **your own trailing 30-day baseline**, not a population.
Whoop data arrives server-side (OAuth + webhooks). Watch/phone data arrives through a
thin device shim posting daily aggregates. Postgres computes all the statistics.

```
contracts/   OpenAPI spec — the single source of truth for both clients
backend/     Go + chi + pgx. Ingest, normalize, serve. Baselines are SQL views.
web/         React + TS + Vite + Recharts. Dark instrument UI, mock-data fallback.
android/     Kotlin + Compose + Health Connect + WorkManager. Dashboard AND device shim.
```

## Run the backend

```bash
createdb longevity
psql longevity < backend/migrations/0001_init.sql
cd backend && cp .env.example .env   # fill in Whoop credentials
go mod tidy && go run ./cmd/api
```

Connect Whoop: create an app at developer.whoop.com, set the redirect URL from
`.env`, then open `http://localhost:8080/v1/connect/whoop`. For webhooks in local
dev, tunnel with `cloudflared tunnel --url http://localhost:8080` (or ngrok) and
register `<tunnel>/v1/webhooks/whoop` (v2 model) in the Whoop dashboard.

## Run the web dashboard

```bash
cd web && npm install && npm run dev
```

Opens on mock data immediately (design without any device connected);
`/v1` proxies to the backend when it's running.

## Run the Android app

Open `android/` in Android Studio, run on a device with Health Connect.
It requests read permissions, schedules a 6-hour WorkManager sync, and posts
daily aggregates to `/v1/ingest/samples` (`10.0.2.2:8080` from the emulator).
Set `INGEST_TOKEN` in `app/build.gradle.kts` to match your `.env`.

## Verify-before-trust list (things to check against live docs on first run)

- Whoop v2 API base path (`/developer/v2/...`) and the recovery-by-sleep-UUID route.
- Whoop webhook signature header names (`X-WHOOP-Signature`, `X-WHOOP-Signature-Timestamp`).
- Health Connect record class names against the connect-client version you resolve.

## Design decisions worth keeping

- `raw_events` is never deleted; `daily_metrics` can always be rebuilt from it.
- Source-of-truth per metric (Whoop wins physiology, watch wins activity) lives in
  ONE view: `metric_preferred`. Change policy there only.
- Baselines exclude the current day (`ROWS BETWEEN 30 PRECEDING AND 1 PRECEDING`)
  so today's anomaly can't dilute the norm it's judged against.
- Whoop rMSSD and Apple SDNN are different HRV statistics — stored as different
  metrics, never charted on one line.
