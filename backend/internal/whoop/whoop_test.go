package whoop

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func newTestClient() *Client {
	return New("client-id", "client-secret", "http://localhost/cb", nil)
}

// --- Signature verification -------------------------------------------------
// Docs (developer.whoop.com/docs/developing/webhooks) define:
// base64(HMAC-SHA256(secret, timestamp + raw_body)). No official test vector is
// published, so we use a synthetic one computed with the same formula.

func TestVerifySignature(t *testing.T) {
	c := newTestClient()
	body := []byte(`{"user_id":10129,"id":"84cbb4e8-1b3f-44e5-b5a7-4b1d0a4f7a4c","type":"sleep.updated","trace_id":"t"}`)
	ts := "1751414400000" // ms since epoch, as WHOOP sends

	mac := hmac.New(sha256.New, []byte(c.Secret))
	mac.Write([]byte(ts + string(body)))
	good := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !c.VerifySignature(ts, body, good) {
		t.Fatal("valid signature rejected")
	}
	if c.VerifySignature("1751414400001", body, good) {
		t.Fatal("accepted signature with wrong timestamp")
	}
	if c.VerifySignature(ts, append(body, 'x'), good) {
		t.Fatal("accepted signature with tampered body")
	}
	if c.VerifySignature(ts, body, "") || c.VerifySignature("", body, good) {
		t.Fatal("accepted empty signature or timestamp")
	}
}

// --- OAuth state -------------------------------------------------------------

func TestStateSingleUse(t *testing.T) {
	c := newTestClient()
	s, err := c.NewState()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 32 {
		t.Fatalf("state should be 16 random bytes hex-encoded, got %q", s)
	}
	if c.ConsumeState("not-issued") {
		t.Fatal("unknown state accepted")
	}
	if !c.ConsumeState(s) {
		t.Fatal("freshly issued state rejected")
	}
	if c.ConsumeState(s) {
		t.Fatal("state accepted twice (must be single-use)")
	}
	if c.ConsumeState("") {
		t.Fatal("empty state accepted")
	}
}

func TestStateExpiry(t *testing.T) {
	c := newTestClient()
	s, _ := c.NewState()
	c.mu.Lock()
	c.states[s] = time.Now().Add(-time.Second) // force expiry
	c.mu.Unlock()
	if c.ConsumeState(s) {
		t.Fatal("expired state accepted")
	}
}

func TestAuthorizeURLCarriesState(t *testing.T) {
	c := newTestClient()
	u := c.AuthorizeURL("abc123")
	req, _ := http.NewRequest("GET", u, nil)
	q := req.URL.Query()
	if q.Get("state") != "abc123" || q.Get("client_id") != "client-id" || q.Get("response_type") != "code" {
		t.Fatalf("bad authorize url: %s", u)
	}
}

// --- Token refresh rotation ---------------------------------------------------
// Emulates the oauth_tokens row + FOR UPDATE lock in memory and drives
// refreshLocked (the exact code AccessToken runs inside its transaction).

type tokenRow struct {
	access, refresh string
	expires         time.Time
}

type fakeTx struct {
	row *tokenRow
}

type fakeRow struct{ r tokenRow }

func (f fakeRow) Scan(dest ...any) error {
	*dest[0].(*string) = f.r.access
	*dest[1].(*string) = f.r.refresh
	*dest[2].(*time.Time) = f.r.expires
	return nil
}

func (f *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return fakeRow{*f.row}
}

func (f *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.row.access = args[0].(string)
	f.row.refresh = args[1].(string)
	f.row.expires = args[2].(time.Time)
	return pgconn.CommandTag{}, nil
}

func TestRefreshRotation(t *testing.T) {
	var mu sync.Mutex // stands in for the row lock (FOR UPDATE)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock() // detect the invariant: refresh must run under the row lock
		hits++
		n := hits
		sent := r.Form.Get("refresh_token")
		want := fmt.Sprintf("refresh-%d", n-1)
		mu.Unlock()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("scope") != "offline" {
			t.Errorf("bad refresh form: %v", r.Form)
		}
		if sent != want {
			// A rotated (stale) refresh token was reused — the real WHOOP
			// endpoint would reject this and kill the integration.
			t.Errorf("stale refresh token reused: got %s want %s", sent, want)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  fmt.Sprintf("access-%d", n),
			"refresh_token": fmt.Sprintf("refresh-%d", n),
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	c := newTestClient()
	c.TokenURL = srv.URL
	row := &tokenRow{access: "access-0", refresh: "refresh-0", expires: time.Now().Add(-time.Minute)}

	// N concurrent callers, each holding the "row lock" around refreshLocked,
	// exactly as accessToken's transaction does. Only the first should refresh.
	var rowLock sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rowLock.Lock()
			defer rowLock.Unlock()
			tok, err := c.refreshLocked(context.Background(), &fakeTx{row: row}, false)
			if err != nil {
				t.Error(err)
			}
			if tok != "access-1" {
				t.Errorf("got token %s, want access-1", tok)
			}
		}()
	}
	wg.Wait()

	if hits != 1 {
		t.Fatalf("token endpoint hit %d times; rotation means it must be exactly 1", hits)
	}
	if row.refresh != "refresh-1" {
		t.Fatalf("rotated refresh token not persisted: %s", row.refresh)
	}

	// force=true must refresh even though the stored token is still fresh.
	rowLock.Lock()
	tok, err := c.refreshLocked(context.Background(), &fakeTx{row: row}, true)
	rowLock.Unlock()
	if err != nil || tok != "access-2" || row.refresh != "refresh-2" {
		t.Fatalf("forced refresh: tok=%s refresh=%s err=%v", tok, row.refresh, err)
	}
}

// --- Get: auto refresh-and-retry on 401 ---------------------------------------

func TestGetRetriesOn401(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer api.Close()

	c := newTestClient()
	c.APIBase = api.URL
	var forces []bool
	c.getToken = func(ctx context.Context, force bool) (string, error) {
		forces = append(forces, force)
		if force {
			return "fresh", nil
		}
		return "stale", nil
	}

	body, err := c.Get(context.Background(), "/activity/sleep/x")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body: %s", body)
	}
	if len(forces) != 2 || forces[0] || !forces[1] {
		t.Fatalf("expected non-forced then forced token fetch, got %v", forces)
	}
}

// --- GetRecoveryForSleep sleep->cycle->recovery resolution --------------------

func TestGetRecoveryForSleep(t *testing.T) {
	var paths []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/activity/sleep/uuid-1":
			_, _ = w.Write([]byte(`{"id":"uuid-1","cycle_id":93845,"nap":false}`))
		case "/cycle/93845/recovery":
			_, _ = w.Write([]byte(`{"cycle_id":93845,"sleep_id":"uuid-1","score":{"recovery_score":68}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	c := newTestClient()
	c.APIBase = api.URL
	c.getToken = func(ctx context.Context, force bool) (string, error) { return "t", nil }

	sleep, rec, err := c.GetRecoveryForSleep(context.Background(), "uuid-1")
	if err != nil {
		t.Fatal(err)
	}
	if sleep == nil || rec == nil {
		t.Fatal("missing payloads")
	}
	var got struct {
		SleepID string `json:"sleep_id"`
	}
	_ = json.Unmarshal(rec, &got)
	if got.SleepID != "uuid-1" {
		t.Fatalf("recovery payload: %s", rec)
	}
	if len(paths) != 2 || paths[0] != "/activity/sleep/uuid-1" || paths[1] != "/cycle/93845/recovery" {
		t.Fatalf("paths: %v", paths)
	}
}
