package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"spotis/internal/config"
	"spotis/internal/crypto"
	"spotis/internal/server"
	"spotis/internal/store"
	"spotis/internal/util"
)

// fakeSpotify stands in for accounts.spotify.com and api.spotify.com.
type fakeSpotify struct {
	*httptest.Server
	mu        sync.Mutex
	lastAPI   string // bearer seen by the last /v1 call
	refreshes int
	banned    bool // /v1/me answers 403 (user not allowlisted)
	scope     string
}

func newFake(t *testing.T) *fakeSpotify {
	f := &fakeSpotify{scope: "user-read-private"}
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != "real-id" {
			http.Error(w, "bad client", 400)
			return
		}
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=UPCODE&state="+url.QueryEscape(q.Get("state")), 302)
	})
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		if u, p, _ := r.BasicAuth(); u != "real-id" || p != "real-secret" {
			w.WriteHeader(401)
			return
		}
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "UPCODE" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"up-access-1","token_type":"Bearer","scope":"` + f.scope + `","expires_in":3600,"refresh_token":"up-refresh"}`))
		case "refresh_token":
			f.mu.Lock()
			f.refreshes++
			f.mu.Unlock()
			_, _ = w.Write([]byte(`{"access_token":"up-access-2","token_type":"Bearer","scope":"user-read-private","expires_in":3600}`))
		case "client_credentials":
			_, _ = w.Write([]byte(`{"access_token":"up-cc","token_type":"Bearer","expires_in":3600}`))
		}
	})
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if f.banned {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"alice","display_name":"Alice"}`))
	})
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.lastAPI = r.Header.Get("Authorization")
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tracks":{"href":"` + f.URL + `/v1/search?q=x","next":"` + f.URL + `/v1/search?q=x&offset=5"}}`))
	})
	mux.HandleFunc("/v1/me/player/currently-playing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_playing":true,"progress_ms":1234,"currently_playing_type":"track","item":{"id":"t1","name":"Song","duration_ms":200000,
			"external_urls":{"spotify":"https://open.spotify.com/track/t1"},"artists":[{"name":"A"},{"name":"B"}],"album":{"name":"Alb","images":[{"url":"http://img/1.jpg"}]}}}`))
	})
	mux.HandleFunc("/v1/limited", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(429)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

type env struct {
	t    *testing.T
	base string
	fake *fakeSpotify
	cli  *http.Client
}

func newEnv(t *testing.T) *env {
	fake := newFake(t)
	dir := t.TempDir()
	st, err := store.Open(dir + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := crypto.New("test-key", dir)
	ts := httptest.NewUnstartedServer(nil)
	cfg := config.Config{PublicURL: "http://" + ts.Listener.Addr().String(), AccountsURL: fake.URL, APIURL: fake.URL, RetentionDays: 7}
	ts.Config.Handler = server.New(cfg, st, box)
	ts.Start()
	t.Cleanup(ts.Close)
	return &env{t: t, base: ts.URL, fake: fake,
		cli: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (e *env) do(method, path string, body any, hdr map[string]string) (*http.Response, []byte) {
	e.t.Helper()
	var rd io.Reader
	if s, ok := body.(string); ok {
		rd = strings.NewReader(s)
	} else if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.base+path, rd)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := e.cli.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

var dash = map[string]string{"Remote-User": "owner", "Content-Type": "application/json"}

func (e *env) want(resp *http.Response, code int, b []byte) {
	e.t.Helper()
	if resp.StatusCode != code {
		e.t.Fatalf("status %d, want %d: %s", resp.StatusCode, code, b)
	}
}

func jsonOf(b []byte) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// login runs /authorize -> fake Spotify -> /oauth/callback and returns the client-side redirect URL.
func (e *env) login(clientID, redirect, extra string) *url.URL {
	e.t.Helper()
	resp, b := e.do("GET", "/authorize?response_type=code&client_id="+clientID+"&redirect_uri="+url.QueryEscape(redirect)+"&state=S1&scope=user-read-private"+extra, nil, nil)
	e.want(resp, 302, b)
	up, _ := e.cli.Get(resp.Header.Get("Location")) // fake Spotify "approves"
	up.Body.Close()
	cb, _ := url.Parse(up.Header.Get("Location"))
	resp, b = e.do("GET", cb.RequestURI(), nil, nil)
	if resp.StatusCode != 302 {
		return nil
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	return loc
}

func TestFullFlow(t *testing.T) {
	e := newEnv(t)

	// Dashboard: closed without the Tinyauth identity header.
	resp, b := e.do("GET", "/dashboard/api/apps", nil, nil)
	e.want(resp, 401, b)
	resp, b = e.do("GET", "/dashboard", nil, nil)
	e.want(resp, 401, b)

	// Upstream not configured yet -> clean OAuth error, then configure + test.
	resp, b = e.do("PUT", "/dashboard/api/upstream", map[string]string{"client_id": "real-id", "client_secret": "real-secret"}, dash)
	e.want(resp, 200, b)
	resp, b = e.do("POST", "/dashboard/api/upstream/test", nil, dash)
	e.want(resp, 200, b)
	if jsonOf(b)["ok"] != true {
		t.Fatalf("upstream test failed: %s", b)
	}

	// Cross-origin writes are rejected.
	resp, b = e.do("POST", "/dashboard/api/apps", map[string]string{"name": "x"}, map[string]string{"Remote-User": "o", "Origin": "https://evil.example"})
	e.want(resp, 403, b)

	// Create a virtual app; bad redirect rejected.
	resp, b = e.do("POST", "/dashboard/api/apps", map[string]any{"name": "bad", "redirect_uris": []string{"http://example.com/cb"}}, dash)
	e.want(resp, 400, b)
	resp, b = e.do("POST", "/dashboard/api/apps", map[string]any{"name": "demo", "redirect_uris": []string{"http://127.0.0.1/cb"}}, dash)
	e.want(resp, 201, b)
	m := jsonOf(b)
	id := m["app"].(map[string]any)["id"].(string)
	secret := m["client_secret"].(string)
	redirect := "http://127.0.0.1:9999/cb" // loopback: any port matches

	// Unregistered redirect URI -> error page, never a redirect.
	resp, b = e.do("GET", "/authorize?response_type=code&client_id="+id+"&redirect_uri=http%3A%2F%2F127.0.0.1%3A1%2Fother", nil, nil)
	e.want(resp, 400, b)

	// Authorization code + PKCE (public client, no secret).
	verifier := util.RandToken(48)
	loc := e.login(id, redirect, "&code_challenge_method=S256&code_challenge="+util.PKCEChallenge(verifier))
	if loc == nil || loc.Query().Get("state") != "S1" || loc.Query().Get("code") == "" {
		t.Fatalf("bad client redirect: %v", loc)
	}
	code := loc.Query().Get("code")
	form := func(kv ...string) string {
		v := url.Values{}
		for i := 0; i < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v.Encode()
	}
	formH := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	resp, b = e.do("POST", "/api/token", form("grant_type", "authorization_code", "code", code, "redirect_uri", redirect, "client_id", id, "code_verifier", "wrong"), formH)
	e.want(resp, 400, b) // wrong verifier (also consumes the code)
	loc = e.login(id, redirect, "&code_challenge_method=S256&code_challenge="+util.PKCEChallenge(verifier))
	code = loc.Query().Get("code")
	resp, b = e.do("POST", "/api/token", form("grant_type", "authorization_code", "code", code, "redirect_uri", redirect, "client_id", id, "code_verifier", verifier), formH)
	e.want(resp, 200, b)
	tok := jsonOf(b)
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)
	if tok["token_type"] != "Bearer" || tok["scope"] != "user-read-private" || access == "" || refresh == "" {
		t.Fatalf("bad token response: %s", b)
	}
	resp, b = e.do("POST", "/api/token", form("grant_type", "authorization_code", "code", code, "redirect_uri", redirect, "client_id", id, "code_verifier", verifier), formH)
	e.want(resp, 400, b) // codes are single use

	// API proxy: swaps the token, rewrites paging URLs to us.
	resp, b = e.do("GET", "/v1/search?q=x", nil, map[string]string{"Authorization": "Bearer " + access})
	e.want(resp, 200, b)
	if e.fake.lastAPI != "Bearer up-access-1" {
		t.Fatalf("upstream saw %q", e.fake.lastAPI)
	}
	if strings.Contains(string(b), e.fake.URL) || !strings.Contains(string(b), e.base+"/v1/search?q=x&offset=5") {
		t.Fatalf("paging URL not rewritten: %s", b)
	}
	resp, b = e.do("GET", "/v1/search", nil, nil)
	e.want(resp, 401, b)
	resp, b = e.do("GET", "/v1/search", nil, map[string]string{"Authorization": "Bearer nope"})
	e.want(resp, 401, b)

	// Refresh (no secret needed for PKCE clients) and the upstream token is reused from cache.
	resp, b = e.do("POST", "/api/token", form("grant_type", "refresh_token", "refresh_token", refresh, "client_id", id), formH)
	e.want(resp, 200, b)
	access2 := jsonOf(b)["access_token"].(string)
	resp, b = e.do("GET", "/v1/search?q=x", nil, map[string]string{"Authorization": "Bearer " + access2})
	e.want(resp, 200, b)

	// Client credentials needs the secret; rotation invalidates the old one.
	basic := func(s string) map[string]string {
		return map[string]string{"Authorization": "Basic " + b64(id+":"+s), "Content-Type": "application/x-www-form-urlencoded"}
	}
	resp, b = e.do("POST", "/api/token", "grant_type=client_credentials", basic(secret))
	e.want(resp, 200, b)
	ccTok := jsonOf(b)["access_token"].(string)
	resp, b = e.do("GET", "/v1/search?q=x", nil, map[string]string{"Authorization": "Bearer " + ccTok})
	e.want(resp, 200, b)
	if e.fake.lastAPI != "Bearer up-cc" {
		t.Fatalf("client-credentials call used %q", e.fake.lastAPI)
	}
	resp, b = e.do("POST", "/api/token", "grant_type=client_credentials", basic("wrong"))
	e.want(resp, 401, b)
	resp, b = e.do("POST", "/dashboard/api/apps/"+id+"/rotate-secret", nil, dash)
	e.want(resp, 200, b)
	newSecret := jsonOf(b)["client_secret"].(string)
	resp, b = e.do("POST", "/api/token", "grant_type=client_credentials", basic(secret))
	e.want(resp, 401, b)
	resp, b = e.do("POST", "/api/token", "grant_type=client_credentials", basic(newSecret))
	e.want(resp, 200, b)

	// Connected users + revoke.
	resp, b = e.do("GET", "/dashboard/api/grants?app="+id, nil, dash)
	e.want(resp, 200, b)
	var gs []map[string]any
	_ = json.Unmarshal(b, &gs)
	if len(gs) != 1 || gs[0]["spotify_id"] != "alice" {
		t.Fatalf("grants: %s", b)
	}

	// Now-playing endpoint: API key, no OAuth. Needs the right scope on the grant.
	gid := itoa(gs[0]["id"])
	resp, b = e.do("POST", "/dashboard/api/grants/"+gid+"/keys", map[string]string{"label": "desk"}, dash)
	e.want(resp, 201, b)
	apiKey := jsonOf(b)["key"].(string)
	keyID := itoa(jsonOf(b)["key_info"].(map[string]any)["id"])
	resp, b = e.do("GET", "/now-playing", nil, nil)
	e.want(resp, 401, b)
	resp, b = e.do("GET", "/now-playing", nil, map[string]string{"X-API-Key": "spk_nope"})
	e.want(resp, 401, b)
	resp, b = e.do("GET", "/now-playing", nil, map[string]string{"X-API-Key": apiKey})
	e.want(resp, 403, b) // grant lacks user-read-currently-playing
	e.fake.scope = "user-read-private user-read-currently-playing"
	if e.login(id, redirect, "") == nil { // re-login upgrades the same grant, key survives
		t.Fatal("re-login failed")
	}
	resp, b = e.do("GET", "/now-playing?key="+apiKey, nil, nil)
	e.want(resp, 200, b)
	np := jsonOf(b)
	it, _ := np["item"].(map[string]any)
	if np["is_playing"] != true || it["name"] != "Song" || it["album"] != "Alb" || it["image"] != "http://img/1.jpg" || len(it["artists"].([]any)) != 2 {
		t.Fatalf("now-playing: %s", b)
	}
	resp, b = e.do("GET", "/now-playing", nil, map[string]string{"Authorization": "Bearer " + apiKey})
	e.want(resp, 200, b)
	resp, b = e.do("DELETE", "/dashboard/api/keys/"+keyID, nil, dash)
	e.want(resp, 204, b)
	resp, b = e.do("GET", "/now-playing", nil, map[string]string{"X-API-Key": apiKey})
	e.want(resp, 401, b)

	resp, b = e.do("DELETE", "/dashboard/api/grants/"+itoa(gs[0]["id"]), nil, dash)
	e.want(resp, 204, b)
	resp, b = e.do("GET", "/v1/search?q=x", nil, map[string]string{"Authorization": "Bearer " + access2})
	e.want(resp, 401, b)
	resp, b = e.do("POST", "/api/token", form("grant_type", "refresh_token", "refresh_token", refresh, "client_id", id), formH)
	e.want(resp, 400, b)

	// Per-app rate limit.
	resp, b = e.do("PATCH", "/dashboard/api/apps/"+id, map[string]any{"rate_limit_per_min": 1}, dash)
	e.want(resp, 200, b)
	resp, b = e.do("POST", "/api/token", "grant_type=client_credentials", basic(newSecret))
	cc := jsonOf(b)["access_token"].(string)
	resp, b = e.do("GET", "/v1/search", nil, map[string]string{"Authorization": "Bearer " + cc})
	e.want(resp, 200, b)
	resp, b = e.do("GET", "/v1/search", nil, map[string]string{"Authorization": "Bearer " + cc})
	e.want(resp, 429, b)
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
	e.do("PATCH", "/dashboard/api/apps/"+id, map[string]any{"rate_limit_per_min": 0}, dash)

	// Shared upstream quota: a 429 from Spotify pauses every app.
	resp, b = e.do("GET", "/v1/limited", nil, map[string]string{"Authorization": "Bearer " + cc})
	e.want(resp, 429, b)
	resp, b = e.do("GET", "/v1/search", nil, map[string]string{"Authorization": "Bearer " + cc})
	e.want(resp, 429, b)

	// A Spotify account that isn't allowlisted upstream gets a clear page.
	e.fake.banned = true
	resp2, b2 := e.do("GET", "/authorize?response_type=code&client_id="+id+"&redirect_uri="+url.QueryEscape(redirect)+"&state=S2", nil, nil)
	up, _ := e.cli.Get(resp2.Header.Get("Location"))
	up.Body.Close()
	cb, _ := url.Parse(up.Header.Get("Location"))
	resp, b = e.do("GET", cb.RequestURI(), nil, nil)
	_ = b2
	e.want(resp, 403, b)
	if !strings.Contains(string(b), "Developer Portal") {
		t.Fatalf("allowlist page missing explanation: %s", b)
	}

	// Request log reaches the dashboard.
	time.Sleep(1500 * time.Millisecond)
	resp, b = e.do("GET", "/dashboard/api/requests?app="+id, nil, dash)
	e.want(resp, 200, b)
	if strings.Contains(string(b), "spk_") {
		t.Fatalf("API key leaked into request log: %s", b)
	}
	if !strings.Contains(string(b), "/v1/search") {
		t.Fatalf("requests missing: %s", b)
	}
	resp, b = e.do("GET", "/dashboard/api/usage?range=1h", nil, dash)
	e.want(resp, 200, b)
	if jsonOf(b)["total"].(float64) < 5 {
		t.Fatalf("usage: %s", b)
	}

	// Deleting the app kills its tokens.
	resp, b = e.do("DELETE", "/dashboard/api/apps/"+id, nil, dash)
	e.want(resp, 204, b)
	resp, b = e.do("GET", "/v1/search", nil, map[string]string{"Authorization": "Bearer " + cc})
	e.want(resp, 401, b)
}
