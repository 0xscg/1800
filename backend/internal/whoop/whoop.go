// Package whoop: OAuth (with rotating refresh tokens), API client, webhook receiver.
//
// Endpoint paths follow the v2 API. Confirm against https://developer.whoop.com/api/
// before first run — WHOOP has restructured paths before (v1 -> v2).
package whoop

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sushan/longevity/internal/store"
)

const (
	authURL  = "https://api.prod.whoop.com/oauth/oauth2/auth"
	tokenURL = "https://api.prod.whoop.com/oauth/oauth2/token"
	apiBase  = "https://api.prod.whoop.com/developer/v2"
	// offline => refresh token. Without it you re-auth every hour.
	scopes = "offline read:recovery read:sleep read:cycles read:workout read:body_measurement read:profile"
)

type Client struct {
	ID, Secret, RedirectURL string
	Store                   *store.Store
	HTTP                    *http.Client
}

func New(id, secret, redirect string, st *store.Store) *Client {
	return &Client{ID: id, Secret: secret, RedirectURL: redirect, Store: st, HTTP: &http.Client{Timeout: 15 * time.Second}}
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
	return authURL + "?" + q.Encode()
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
//
// WHOOP rotates refresh tokens: each refresh INVALIDATES the previous one.
// The row lock (FOR UPDATE) makes concurrent refreshes safe — the second
// caller waits, then sees the fresh token and skips its own refresh.
func (c *Client) AccessToken(ctx context.Context) (string, error) {
	tx, err := c.Store.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var access, refresh string
	var expires time.Time
	err = tx.QueryRow(ctx,
		`SELECT access_token, refresh_token, expires_at FROM oauth_tokens WHERE provider='whoop' FOR UPDATE`,
	).Scan(&access, &refresh, &expires)
	if err != nil {
		return "", errors.New("whoop not connected: visit /v1/connect/whoop")
	}

	if time.Until(expires) > 5*time.Minute {
		return access, tx.Commit(ctx)
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
	return tr.AccessToken, tx.Commit(ctx)
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
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
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
	return &tr, nil
}

// Get fetches an API path (e.g. "/activity/sleep/"+uuid) and returns raw JSON.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	token, err := c.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("whoop GET %s: %d %s", path, resp.StatusCode, body)
	}
	return body, nil
}

// VerifySignature checks the webhook HMAC.
// WHOOP signs base64(HMAC-SHA256(client_secret, timestamp + raw_body)) into
// X-WHOOP-Signature, with X-WHOOP-Signature-Timestamp alongside.
// Confirm header names against current docs on first run.
func (c *Client) VerifySignature(timestamp string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(c.Secret))
	mac.Write([]byte(timestamp))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// WebhookEvent is the v2 webhook body (UUID ids for sleep/workout;
// recovery events carry the UUID of the associated sleep).
type WebhookEvent struct {
	UserID  int64           `json:"user_id"`
	ID      json.RawMessage `json:"id"`
	Type    string          `json:"type"` // sleep.updated, recovery.updated, workout.updated, ...
	TraceID string          `json:"trace_id"`
}

func (e WebhookEvent) IDString() string {
	return strings.Trim(string(e.ID), `"`)
}

// Paginated fetch of a collection endpoint into raw pages (for backfill).
func (c *Client) FetchCollection(ctx context.Context, path string, limitPages int, onPage func(page []byte) error) error {
	next := ""
	for i := 0; i < limitPages; i++ {
		p := path
		if next != "" {
			p += "?nextToken=" + url.QueryEscape(next)
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

