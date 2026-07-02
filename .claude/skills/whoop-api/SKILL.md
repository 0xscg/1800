---
name: whoop-api
description: Working with the WHOOP v2 API in this project — OAuth with rotating refresh tokens, webhook verification, endpoint paths, cycle-based data model, and historical backfill. Use this skill whenever touching backend/internal/whoop, debugging token or webhook failures, adding Whoop data types (workouts, cycles, body measurements), or writing any code that calls api.prod.whoop.com. Also use when the user mentions Whoop, recovery, strain, or sleep ingestion — even for seemingly small changes, because the token-rotation and cycle-model gotchas are easy to break silently.
---

# WHOOP v2 API

## The two gotchas that break silently

**1. Refresh tokens rotate.** Every refresh returns a NEW refresh token and invalidates
the old one. If two goroutines refresh concurrently with the same old token, the second
fails and the stored token may be dead → integration silently disconnects until manual
re-auth. This is why `AccessToken()` in `internal/whoop/whoop.go` does the refresh inside
a `SELECT ... FOR UPDATE` transaction on the `oauth_tokens` row. Never move the refresh
outside that lock, never cache tokens in memory across requests, and always request the
`offline` scope on refresh so the new grant includes a refresh token.

**2. Data is cycle-based, not calendar-based.** WHOOP models sleep cycles, recovery
cycles (generated only after a sleep completes), and strain cycles. Recovery has no
independent date — v2 recovery webhooks carry the UUID of the *associated sleep*.
Day attribution is OUR decision: this project assigns sleep and its recovery to the
wake-up day (see `normalize.WhoopSleep.Day()`). Keep that rule consistent everywhere.

## Endpoints (verify paths on first touch — they restructured in v1→v2)

- Base: `https://api.prod.whoop.com/developer/v2`
- Auth: `https://api.prod.whoop.com/oauth/oauth2/auth`, token: `.../oauth/oauth2/token`
- Collections (paginated via `next_token`): `/activity/sleep`, `/activity/workout`, `/cycle`
- Single records by UUID: `/activity/sleep/{uuid}`, `/activity/workout/{uuid}`
- Recovery for a sleep: `/recovery/sleep/{uuid}` (CONFIRM exact route in docs)
- Scopes used: `offline read:recovery read:sleep read:cycles read:workout read:body_measurement read:profile`
- Rate limits exist; a full day of member data is ~4 KB, so backfill politely (sequential, small pages).

## Webhooks

- v2 model only (v1 removed). Configure per-URL model version in the developer dashboard.
- Signature: base64(HMAC-SHA256(client_secret, timestamp + raw_body)) in `X-WHOOP-Signature`,
  timestamp in `X-WHOOP-Signature-Timestamp`. Implemented in `Client.VerifySignature`.
- Ack 200 immediately, process async (WHOOP retries ~5 times over an hour on non-2xx/slow).
- Events: `sleep.updated`, `recovery.updated`, `workout.updated` (creates arrive as updates).
- Webhook bodies contain IDs only — always fetch the full record afterwards.
- Local dev needs a tunnel (cloudflared/ngrok) since WHOOP must reach the webhook URL.

## Adding a backfill command

Pattern: `FetchCollection(ctx, "/activity/sleep", pages, onPage)` → for each record in the
page envelope's `records` array: `UpsertRawEvent` then the matching `normalize.X` call.
Idempotency makes re-running safe. Backfill sleep BEFORE recovery so day attribution has
its anchor. See references/payloads.md for the field shapes normalize expects.
