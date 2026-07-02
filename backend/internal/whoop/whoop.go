// Package whoop: OAuth (with rotating refresh tokens), API client, webhook receiver.
//
// Endpoint paths verified against https://developer.whoop.com/api/ (v2) and
// https://developer.whoop.com/docs/developing/webhooks/ on 2026-07-02:
//   - base https://api.prod.whoop.com/developer/v2
//   - sleep/workout by UUID under /activity/...; recovery has NO by-sleep route —
//     fetch the sleep, read its cycle_id, then GET /cycle/{cycleId}/recovery.
//   - webhook signature: base64(HMAC-SHA256(secret, timestamp + raw_body)) in
//     X-WHOOP-Signature, ms-epoch timestamp in X-WHOOP-Signature-Timestamp.
package whoop

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sushan/longevity/internal/store"
)

const (
	defaultAuthURL  = "https://api.prod.whoop.com/oauth/oauth2/auth"
	defaultTokenURL = "https://api.prod.whoop.com/oauth/oauth2/token"
	defaultAPIBase  = "https://api.prod.whoop.com/developer/v2"
	// offline => refresh token. Without it you re-auth every hour.
	scopes = "offline read:recovery read:sleep read:cycles read:workout read:body_measurement read:profile"

	stateTTL = 10 * time.Minute
)

type Client struct {
	ID, Secret, RedirectURL string
	Store                   *store.Store
	HTTP                    *http.Client

	// Endpoint overrides (defaulted in New; settable in tests).
	AuthURL, TokenURL, APIBase string

	// getToken is the token source used by Get; defaults to c.accessToken.
	// Overridable in tests so Get's 401-retry can be exercised without a DB.
	getToken func(ctx context.Context, force bool) (string, error)

	// now is the clock used for webhook timestamp checks; time.Now by default,
	// injectable in tests.
	now func() time.Time

	mu     sync.Mutex
	states map[string]time.Time // pending OAuth states -> expiry
}

func New(id, secret, redirect string, st *store.Store) *Client {
	c := &Client{
		ID: id, Secret: secret, RedirectURL: redirect, Store: st,
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		AuthURL: defaultAuthURL, TokenURL: defaultTokenURL, APIBase: defaultAPIBase,
		states: map[string]time.Time{},
	}
	c.getToken = c.accessToken
	c.now = time.Now
	return c
}

// SetTokenSource overrides the token source used by Get (tests only).
func (c *Client) SetTokenSource(fn func(ctx context.Context, force bool) (string, error)) {
	c.getToken = fn
}

// NewState mints a single-use random state for the OAuth redirect.
func (c *Client) NewState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := hex.EncodeToString(b)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, exp := range c.states { // opportunistic GC of expired states
		if now.After(exp) {
			delete(c.states, k)
		}
	}
	c.states[s] = now.Add(stateTTL)
	return s, nil
}

// ConsumeState validates and burns a state (single use).
func (c *Client) ConsumeState(s string) bool {
	if s == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.states[s]
	if !ok {
		return false
	}
	delete(c.states, s)
	return time.Now().Before(exp)
}

// AuthorizeURL builds the consent URL to redirect the browser to.
func (c *Client) AuthorizeURL(state string) string {
	q := url.Values{
		"client_id":     {c.ID},
		"redirect_uri":  {c.RedirectURL},
		"response_type": {"code"},
		"scope":         {scopes},
		"state":         {state},
	}
	return c.AuthURL + "?" + q.Encode()
}

type tokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// Exchange swaps the callback code for tokens and persists them.
func (c *Client) Exchange(ctx context.Context, code string) error {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"redirect_uri":  {c.RedirectURL},
	}
	return c.tokenCall(ctx, form)
}

// AccessToken returns a valid token, refreshing if within 5 min of expiry.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	return c.accessToken(ctx, false)
}

// dbtx is the slice of pgx.Tx the refresh path needs; faked in tests.
type dbtx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// accessToken returns a valid token, refreshing inside a row-locked
// transaction. force skips the expiry check (used after a 401).
//
// WHOOP rotates refresh tokens: each refresh INVALIDATES the previous one.
// The row lock (FOR UPDATE) makes concurrent refreshes safe — the second
// caller waits, then sees the fresh token and skips its own refresh.
func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	tx, err := c.Store.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	token, err := c.refreshLocked(ctx, tx, force)
	if err != nil {
		return "", err
	}
	return token, tx.Commit(ctx)
}

// refreshLocked does the SELECT ... FOR UPDATE / refresh / UPDATE dance on an
// open transaction. Callers own commit/rollback.
func (c *Client) refreshLocked(ctx context.Context, tx dbtx, force bool) (string, error) {
	var access, refresh string
	var expires time.Time
	err := tx.QueryRow(ctx,
		`SELECT access_token, refresh_token, expires_at FROM oauth_tokens WHERE provider='whoop' FOR UPDATE`,
	).Scan(&access, &refresh, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("whoop not connected: visit /v1/connect/whoop")
	}
	if err != nil {
		return "", err
	}

	if !force && time.Until(expires) > 5*time.Minute {
		return access, nil
	}

	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
		"client_id":     {c.ID},
		"client_secret": {c.Secret},
		"scope":         {"offline"}, // request offline again so the new grant includes a refresh token
	}
	tr, err := c.rawTokenCall(ctx, form)
	if err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx,
		`UPDATE oauth_tokens SET access_token=$1, refresh_token=$2, expires_at=$3, updated_at=now() WHERE provider='whoop'`,
		tr.AccessToken, tr.RefreshToken, time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second))
	if err != nil {
		return "", err
	}
	return tr.AccessToken, nil
}

func (c *Client) tokenCall(ctx context.Context, form url.Values) error {
	tr, err := c.rawTokenCall(ctx, form)
	if err != nil {
		return err
	}
	_, err = c.Store.Pool.Exec(ctx, `
		INSERT INTO oauth_tokens (provider, access_token, refresh_token, expires_at)
		VALUES ('whoop', $1, $2, $3)
		ON CONFLICT (provider) DO UPDATE
		SET access_token=EXCLUDED.access_token, refresh_token=EXCLUDED.refresh_token,
		    expires_at=EXCLUDED.expires_at, updated_at=now()`,
		tr.AccessToken, tr.RefreshToken, time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second))
	return err
}

func (c *Client) rawTokenCall(ctx context.Context, form url.Values) (*tokenResp, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("whoop token endpoint %d: %s", resp.StatusCode, body)
	}
	var tr tokenResp
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, err
	}
	if tr.AccessToken == "" || tr.RefreshToken == "" {
		return nil, errors.New("whoop token endpoint returned incomplete grant")
	}
	return &tr, nil
}

// Get fetches an API path (e.g. "/activity/sleep/"+uuid) and returns raw JSON.
// On a 401 (token revoked/expired early) it force-refreshes once and retries.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	body, status, err := c.get(ctx, path, false)
	if err == nil && status == http.StatusUnauthorized {
		body, status, err = c.get(ctx, path, true)
	}
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("whoop GET %s: %d %s", path, status, body)
	}
	return body, nil
}

func (c *Client) get(ctx context.Context, path string, forceRefresh bool) ([]byte, int, error) {
	token, err := c.getToken(ctx, forceRefresh)
	if err != nil {
		return nil, 0, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.APIBase+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

// GetRecoveryForSleep resolves a sleep UUID to its recovery record.
// v2 has no /recovery/sleep/{uuid}: the sleep record carries cycle_id, and
// recovery hangs off the cycle (GET /cycle/{cycleId}/recovery).
// Returns (sleepPayload, recoveryPayload, error).
func (c *Client) GetRecoveryForSleep(ctx context.Context, sleepID string) ([]byte, []byte, error) {
	sleep, err := c.Get(ctx, "/activity/sleep/"+url.PathEscape(sleepID))
	if err != nil {
		return nil, nil, err
	}
	var s struct {
		CycleID int64 `json:"cycle_id"`
	}
	if err := json.Unmarshal(sleep, &s); err != nil || s.CycleID == 0 {
		return sleep, nil, fmt.Errorf("whoop sleep %s: missing cycle_id", sleepID)
	}
	rec, err := c.Get(ctx, fmt.Sprintf("/cycle/%d/recovery", s.CycleID))
	if err != nil {
		return sleep, nil, err
	}
	return sleep, rec, nil
}

// maxWebhookSkew bounds how far the signed timestamp may drift from our clock
// (either direction), limiting the replay window for captured signatures.
const maxWebhookSkew = 5 * time.Minute

// VerifySignature checks the webhook HMAC:
// base64(HMAC-SHA256(client_secret, timestamp + raw_body)) == X-WHOOP-Signature,
// where timestamp is the X-WHOOP-Signature-Timestamp header (ms since epoch).
// Timestamps more than 5 minutes from now (either way) are rejected.
// Verified against developer.whoop.com/docs/developing/webhooks (2026-07-02).
func (c *Client) VerifySignature(timestamp string, body []byte, signature string) bool {
	if timestamp == "" || signature == "" {
		return false
	}
	ms, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	skew := c.now().Sub(time.UnixMilli(ms))
	if skew > maxWebhookSkew || skew < -maxWebhookSkew {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.Secret))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// WebhookEvent is the v2 webhook body. IDs are UUID strings for sleep/workout
// events; recovery events carry the UUID of the ASSOCIATED SLEEP. Event types:
// sleep.updated, sleep.deleted, recovery.updated, recovery.deleted,
// workout.updated, workout.deleted (creates arrive as updates). Bodies contain
// IDs only — fetch the full record afterwards.
type WebhookEvent struct {
	UserID  int64           `json:"user_id"`
	ID      json.RawMessage `json:"id"`
	Type    string          `json:"type"`
	TraceID string          `json:"trace_id"`
}

func (e WebhookEvent) IDString() string {
	return strings.Trim(string(e.ID), `"`)
}

// Paginated fetch of a collection endpoint into raw pages (for backfill).
// Query param is nextToken; response field is next_token (verified v2 docs).
func (c *Client) FetchCollection(ctx context.Context, path string, limitPages int, onPage func(page []byte) error) error {
	next := ""
	for i := 0; i < limitPages; i++ {
		p := path
		if next != "" {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			p += sep + "nextToken=" + url.QueryEscape(next)
		}
		body, err := c.Get(ctx, p)
		if err != nil {
			return err
		}
		if err := onPage(body); err != nil {
			return err
		}
		var envelope struct {
			NextToken string `json:"next_token"`
		}
		_ = json.Unmarshal(body, &envelope)
		if envelope.NextToken == "" {
			return nil
		}
		next = envelope.NextToken
	}
	return nil
}
