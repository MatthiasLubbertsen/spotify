// Package upstream talks to the real Spotify (accounts + Web API) using the single registered app.
package upstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"spotis/internal/crypto"
	"spotis/internal/store"
)

var (
	ErrNotConfigured = errors.New("upstream Spotify app is not configured")
	ErrReauth        = errors.New("upstream refresh token no longer valid")
	ErrForbidden     = errors.New("spotify user is not allowlisted for the upstream app")
)

const (
	SettingClientID     = "upstream_client_id"
	SettingClientSecret = "upstream_client_secret"
)

type Error struct {
	Status     int
	Code, Desc string
}

func (e *Error) Error() string { return fmt.Sprintf("spotify %d %s: %s", e.Status, e.Code, e.Desc) }

type TokenResp struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

type cached struct {
	token string
	exp   time.Time
}

type entry struct {
	mu sync.Mutex
	c  cached
}

type Client struct {
	St          *store.Store
	Box         *crypto.Box
	HTTP        *http.Client
	AccountsURL string
	APIURL      string
	CallbackURL string // our fixed redirect URI registered at Spotify

	mu       sync.Mutex
	grants   map[int64]*entry
	cc       entry
	cooldown atomic.Int64 // unix ms until which upstream is considered rate limited
}

func New(st *store.Store, box *crypto.Box, accounts, api, publicURL string) *Client {
	return &Client{
		St: st, Box: box, AccountsURL: accounts, APIURL: api,
		CallbackURL: publicURL + "/oauth/callback",
		HTTP:        &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second, MaxIdleConnsPerHost: 16}},
		grants:      map[int64]*entry{},
	}
}

// Creds returns the upstream app credentials.
func (c *Client) Creds() (id, secret string, err error) {
	id = c.St.GetSetting(SettingClientID)
	enc := c.St.GetSetting(SettingClientSecret)
	if id == "" || enc == "" {
		return "", "", ErrNotConfigured
	}
	secret, err = c.Box.Open(enc)
	return id, secret, err
}

func (c *Client) Configured() bool { _, _, err := c.Creds(); return err == nil }

func (c *Client) AuthorizeURL(state, scope string, showDialog bool) (string, error) {
	id, _, err := c.Creds()
	if err != nil {
		return "", err
	}
	q := url.Values{"client_id": {id}, "response_type": {"code"}, "redirect_uri": {c.CallbackURL}, "state": {state}}
	if scope != "" {
		q.Set("scope", scope)
	}
	if showDialog {
		q.Set("show_dialog", "true")
	}
	return c.AccountsURL + "/authorize?" + q.Encode(), nil
}

func (c *Client) token(ctx context.Context, form url.Values) (*TokenResp, error) {
	id, secret, err := c.Creds()
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.AccountsURL+"/api/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(id+":"+secret)))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusTooManyRequests {
		c.SetCooldown(resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode != http.StatusOK {
		e := &Error{Status: resp.StatusCode}
		var j struct {
			Error string `json:"error"`
			Desc  string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &j)
		e.Code, e.Desc = j.Error, j.Desc
		return nil, e
	}
	var t TokenResp
	if err := json.Unmarshal(body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Exchange trades the upstream authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code string) (*TokenResp, error) {
	return c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {c.CallbackURL}})
}

// Me returns the Spotify user id and display name behind an upstream access token.
func (c *Client) Me(ctx context.Context, access string) (id, name string, err error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL+"/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return "", "", ErrForbidden
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", "", fmt.Errorf("GET /v1/me: %d %s", resp.StatusCode, b)
	}
	var j struct {
		ID   string `json:"id"`
		Name string `json:"display_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&j); err != nil || j.ID == "" {
		return "", "", fmt.Errorf("GET /v1/me: bad response")
	}
	return j.ID, j.Name, nil
}

// Test verifies the stored credentials with a client-credentials request.
func (c *Client) Test(ctx context.Context) error {
	_, err := c.token(ctx, url.Values{"grant_type": {"client_credentials"}})
	return err
}

func (c *Client) grantEntry(id int64) *entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.grants[id]
	if e == nil {
		e = &entry{}
		c.grants[id] = e
	}
	return e
}

// Prime caches an access token obtained during login.
func (c *Client) Prime(grantID int64, t *TokenResp) {
	e := c.grantEntry(grantID)
	e.mu.Lock()
	e.c = cached{t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}
	e.mu.Unlock()
}

// Invalidate drops the cached upstream access token for a grant.
func (c *Client) Invalidate(grantID int64) {
	e := c.grantEntry(grantID)
	e.mu.Lock()
	e.c = cached{}
	e.mu.Unlock()
}

// Forget removes cache state for a revoked grant.
func (c *Client) Forget(grantID int64) {
	c.mu.Lock()
	delete(c.grants, grantID)
	c.mu.Unlock()
}

// AccessForGrant returns a valid upstream access token for the grant, refreshing if needed.
func (c *Client) AccessForGrant(ctx context.Context, grantID int64) (string, error) {
	e := c.grantEntry(grantID)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.c.token != "" && time.Until(e.c.exp) > 60*time.Second {
		return e.c.token, nil
	}
	g, err := c.St.GetGrant(grantID)
	if err != nil {
		return "", err
	}
	if g.Status != "ok" {
		return "", ErrReauth
	}
	refresh, err := c.Box.Open(g.RefreshEnc)
	if err != nil {
		return "", err
	}
	t, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	if err != nil {
		var ue *Error
		if errors.As(err, &ue) && ue.Code == "invalid_grant" {
			_ = c.St.SetGrantStatus(grantID, "reauth")
			return "", ErrReauth
		}
		return "", err
	}
	if t.RefreshToken != "" && t.RefreshToken != refresh {
		if enc, err := c.Box.Seal(t.RefreshToken); err == nil {
			_ = c.St.UpdateGrantRefresh(grantID, enc)
		}
	}
	e.c = cached{t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}
	return e.c.token, nil
}

// AccessClientCredentials returns an app-only upstream token.
func (c *Client) AccessClientCredentials(ctx context.Context) (string, error) {
	c.cc.mu.Lock()
	defer c.cc.mu.Unlock()
	if c.cc.c.token != "" && time.Until(c.cc.c.exp) > 60*time.Second {
		return c.cc.c.token, nil
	}
	t, err := c.token(ctx, url.Values{"grant_type": {"client_credentials"}})
	if err != nil {
		return "", err
	}
	c.cc.c = cached{t.AccessToken, time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)}
	return t.AccessToken, nil
}

// InvalidateCC drops the cached app-only token.
func (c *Client) InvalidateCC() {
	c.cc.mu.Lock()
	c.cc.c = cached{}
	c.cc.mu.Unlock()
}

// SetCooldown records an upstream 429. All virtual apps share Spotify's quota,
// so we stop forwarding for the Retry-After period (capped at 5 minutes).
func (c *Client) SetCooldown(retryAfter string) {
	secs, err := strconv.Atoi(strings.TrimSpace(retryAfter))
	if err != nil || secs < 1 {
		secs = 5
	}
	if secs > 300 {
		secs = 300
	}
	c.cooldown.Store(time.Now().Add(time.Duration(secs) * time.Second).UnixMilli())
}

// CooldownLeft returns seconds remaining of the upstream cooldown, or 0.
func (c *Client) CooldownLeft() int {
	ms := c.cooldown.Load() - time.Now().UnixMilli()
	if ms <= 0 {
		return 0
	}
	return int((ms + 999) / 1000)
}
