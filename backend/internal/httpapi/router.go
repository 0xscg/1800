package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
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
	r.Use(middleware.Logger, middleware.Recoverer, cors)

	// --- Whoop connect + webhook ---
	r.Get("/v1/connect/whoop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, a.Whoop.AuthorizeURL("local"), http.StatusFound)
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

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*") // personal tool; tighten if it grows
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) whoopCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	if err := a.Whoop.Exchange(r.Context(), code); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
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

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		a.handleWhoopEvent(ctx, ev)
	}()
}

func (a *API) handleWhoopEvent(ctx context.Context, ev whoop.WebhookEvent) {
	id := ev.IDString()
	switch ev.Type {
	case "sleep.updated":
		payload, err := a.Whoop.Get(ctx, "/activity/sleep/"+id)
		if err != nil {
			return
		}
		_ = a.Store.UpsertRawEvent(ctx, "whoop", "sleep", id, payload)
		_ = normalize.Sleep(ctx, a.Store, payload)
	case "recovery.updated":
		// v2 recovery events carry the UUID of the associated sleep.
		payload, err := a.Whoop.Get(ctx, "/recovery/sleep/"+id) // confirm exact path in docs
		if err != nil {
			return
		}
		_ = a.Store.UpsertRawEvent(ctx, "whoop", "recovery", id, payload)
		_ = normalize.Recovery(ctx, a.Store, payload)
	case "workout.updated":
		payload, err := a.Whoop.Get(ctx, "/activity/workout/"+id)
		if err != nil {
			return
		}
		_ = a.Store.UpsertRawEvent(ctx, "whoop", "workout", id, payload)
	}
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
	raw, _ := json.Marshal(req.Samples)
	_ = a.Store.UpsertRawEvent(r.Context(), "device", "sample_batch", req.BatchID, raw)
	if err := normalize.Device(r.Context(), a.Store, req.Samples); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]int{"accepted": len(req.Samples)})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
