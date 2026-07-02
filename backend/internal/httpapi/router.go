package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/sushan/longevity/internal/config"
	"github.com/sushan/longevity/internal/normalize"
	"github.com/sushan/longevity/internal/store"
	"github.com/sushan/longevity/internal/whoop"
)

type API struct {
	Cfg   config.Config
	Store *store.Store
	Whoop *whoop.Client
}

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger, middleware.Recoverer, corsFor(a.Cfg.WebOrigin))

	// --- Whoop connect + webhook ---
	r.Get("/v1/connect/whoop", func(w http.ResponseWriter, r *http.Request) {
		state, err := a.Whoop.NewState()
		if err != nil {
			http.Error(w, "state", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, a.Whoop.AuthorizeURL(state), http.StatusFound)
	})
	r.Get("/v1/connect/whoop/callback", a.whoopCallback)
	r.Post("/v1/webhooks/whoop", a.whoopWebhook)

	// --- Device shim ingest ---
	r.Post("/v1/ingest/samples", a.ingestSamples)

	// --- Dashboard reads ---
	r.Get("/v1/dashboard/today", a.today)
	r.Get("/v1/metrics/{metric}", a.series)
	r.Get("/v1/annotations", a.listAnnotations)
	r.Post("/v1/annotations", a.createAnnotation)

	return r
}

// corsFor allows only the configured web origin (WEB_ORIGIN). Health data is
// GDPR special category — never wildcard this.
func corsFor(origin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (a *API) whoopCallback(w http.ResponseWriter, r *http.Request) {
	if !a.Whoop.ConsumeState(r.URL.Query().Get("state")) {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	if err := a.Whoop.Exchange(r.Context(), code); err != nil {
		log.Printf("whoop: code exchange failed: %v", err)
		http.Error(w, "whoop token exchange failed", http.StatusBadGateway)
		return
	}
	w.Write([]byte("Whoop connected. You can close this tab."))
}

// Webhook: verify HMAC, ack fast, fetch + normalize in the background.
func (a *API) whoopWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	ts := r.Header.Get("X-WHOOP-Signature-Timestamp")
	sig := r.Header.Get("X-WHOOP-Signature")
	if !a.Whoop.VerifySignature(ts, body, sig) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	var ev whoop.WebhookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	// Ack before doing work; WHOOP retries on non-2xx and slow responses.
	w.WriteHeader(http.StatusOK)

	go a.processWhoopEvent(ev)
}

// whoopRetryWaits: backoff between attempts. Overridable in tests.
// Deliberately in-process only — a durable queue table was considered and
// deferred; an event lost to a full process crash is re-fetchable via backfill.
var whoopRetryWaits = []time.Duration{5 * time.Second, 25 * time.Second}

// processWhoopEvent retries handleWhoopEvent so a transient DB/WHOOP failure
// doesn't permanently drop an already-acked webhook event. Fresh context per
// attempt. GDPR: log type / id / stage only — never payload contents.
func (a *API) processWhoopEvent(ev whoop.WebhookEvent) {
	attempts := len(whoopRetryWaits) + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := a.handleWhoopEvent(ctx, ev)
		cancel()
		if err == nil {
			return
		}
		if attempt < attempts {
			log.Printf("whoop event %s id=%s: attempt %d/%d failed, retrying", ev.Type, ev.IDString(), attempt, attempts)
			time.Sleep(whoopRetryWaits[attempt-1])
		} else {
			log.Printf("whoop event %s id=%s: attempt %d/%d failed, giving up", ev.Type, ev.IDString(), attempt, attempts)
		}
	}
}

// handleWhoopEvent does one processing attempt after the webhook is acked.
// Returns the first error so the caller can retry (all writes are idempotent).
// GDPR: log event type / id / stage / error class only — never payload contents.
func (a *API) handleWhoopEvent(ctx context.Context, ev whoop.WebhookEvent) error {
	id := ev.IDString()
	fail := func(stage string, err error) error {
		log.Printf("whoop event %s id=%s: %s failed: %v", ev.Type, id, stage, err)
		return err
	}
	switch ev.Type {
	case "sleep.updated":
		payload, err := a.Whoop.Get(ctx, "/activity/sleep/"+id)
		if err != nil {
			return fail("fetch", err)
		}
		if err := a.Store.UpsertRawEvent(ctx, "whoop", "sleep", id, payload); err != nil {
			return fail("raw upsert", err)
		}
		if err := normalize.Sleep(ctx, a.Store, payload); err != nil {
			return fail("normalize", err)
		}
	case "recovery.updated":
		// v2 recovery events carry the UUID of the associated sleep. There is
		// no /recovery/sleep/{uuid} route: resolve sleep -> cycle_id -> recovery.
		sleep, recovery, err := a.Whoop.GetRecoveryForSleep(ctx, id)
		var wakeDay time.Time // zero → normalize falls back to created_at
		if sleep != nil {
			// We fetched the sleep anyway; store it (idempotent upsert).
			if err := a.Store.UpsertRawEvent(ctx, "whoop", "sleep", id, sleep); err != nil {
				return fail("sleep raw upsert", err)
			}
			if err := normalize.Sleep(ctx, a.Store, sleep); err != nil {
				return fail("sleep normalize", err)
			}
			if d, err := normalize.SleepWakeDay(sleep); err == nil {
				wakeDay = d
			}
		}
		if err != nil {
			return fail("fetch", err)
		}
		if err := a.Store.UpsertRawEvent(ctx, "whoop", "recovery", id, recovery); err != nil {
			return fail("raw upsert", err)
		}
		if err := normalize.Recovery(ctx, a.Store, recovery, wakeDay); err != nil {
			return fail("normalize", err)
		}
	case "workout.updated":
		payload, err := a.Whoop.Get(ctx, "/activity/workout/"+id)
		if err != nil {
			return fail("fetch", err)
		}
		if err := a.Store.UpsertRawEvent(ctx, "whoop", "workout", id, payload); err != nil {
			return fail("raw upsert", err)
		}
	default:
		log.Printf("whoop event %s id=%s: ignored", ev.Type, id)
	}
	return nil
}

func (a *API) ingestSamples(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	want := "Bearer " + a.Cfg.DeviceIngestToken
	if subtle.ConstantTimeCompare([]byte(auth), []byte(want)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		BatchID string                   `json:"batch_id"`
		Samples []normalize.DeviceSample `json:"samples"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if req.BatchID == "" {
		http.Error(w, "batch_id is required", http.StatusBadRequest)
		return
	}
	// Validate the WHOLE batch before persisting anything: an invalid batch
	// must leave no raw_events row and no partial daily_metrics.
	if err := normalize.ValidateDeviceSamples(req.Samples); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	raw, err := json.Marshal(req.Samples)
	if err != nil {
		http.Error(w, "bad samples", http.StatusBadRequest)
		return
	}
	if err := a.Store.UpsertRawEvent(r.Context(), "device", "sample_batch", req.BatchID, raw); err != nil {
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	if err := normalize.Device(r.Context(), a.Store, req.Samples); err != nil {
		if errors.Is(err, normalize.ErrBadSample) {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, "storage error", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, map[string]int{"accepted": len(req.Samples)})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
