package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeSpotify stands in for accounts.spotify.com + api.spotify.com.
type fakeSpotify struct {
	srv        *httptest.Server
	mu         sync.Mutex
	challenges map[string]string // code -> challenge
	blocked    bool              // simulate a user missing from the dev-mode allowlist
	tokenCalls map[string]int
}

func newFake(t *testing.T) *fakeSpotify {
	f := &fakeSpotify{challenges: map[string]string{}, tokenCalls: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("client_id") != "real-client" || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" {
			http.Error(w, "bad authorize", 400)
			return
		}
		code := randHex(8)
		f.mu.Lock()
		f.challenges[code] = q.Get("code_challenge")
		f.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?code="+code+"&state="+url.QueryEscape(q.Get("state")), 302)
	})
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		g := r.PostForm.Get("grant_type")
		f.mu.Lock()
		f.tokenCalls[g]++
		f.mu.Unlock()
		switch g {
		case "authorization_code":
			f.mu.Lock()
			ch := f.challenges[r.PostForm.Get("code")]
			f.mu.Unlock()
			h := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if ch == "" || base64.RawURLEncoding.EncodeToString(h[:]) != ch || r.PostForm.Get("client_id") != "real-client" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.Write([]byte(`{"access_token":"up-access-1","token_type":"Bearer","scope":"user-read-private","expires_in":3600,"refresh_token":"up-refresh-1"}`))
		case "refresh_token":
			if r.PostForm.Get("refresh_token") != "up-refresh-1" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.Write([]byte(`{"access_token":"up-access-2","token_type":"Bearer","scope":"user-read-private","expires_in":3600}`))
		case "client_credentials":
			if u, p, _ := r.BasicAuth(); u != "real-client" || p != "real-secret" {
				w.WriteHeader(401)
				w.Write([]byte(`{"error":"invalid_client"}`))
				return
			}
			w.Write([]byte(`{"access_token":"up-app","token_type":"Bearer","expires_in":3600}`))
		}
	})
	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		blocked := f.blocked
		f.mu.Unlock()
		if blocked {
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"status":403,"message":"User not registered in the Developer Dashboard"}}`))
			return
		}
		a := r.Header.Get("Authorization")
		if a != "Bearer up-access-1" && a != "Bearer up-access-2" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"status":401,"message":"Invalid access token"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"user1","display_name":"User One"}`))
	})
	mux.HandleFunc("/v1/me/playlists", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"href":"` + f.srv.URL + `/v1/me/playlists","next":"` + f.srv.URL + `/v1/me/playlists?offset=1","items":[]}`))
	})
	mux.HandleFunc("/v1/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer up-app" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"q":"` + r.URL.Query().Get("q") + `"}`))
	})
	mux.HandleFunc("/v1/limited", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":{"status":429,"message":"rate"}}`))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type harness struct {
	t      *testing.T
	s      *Server
	url    string
	fake   *fakeSpotify
	client *http.Client // follows nothing, keeps cookies
}

func newHarness(t *testing.T) *harness {
	f := newFake(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	cfg := &Config{
		PublicURL: "http://" + ln.Addr().String(), UserHeader: "Remote-User", AllowedUsers: map[string]bool{},
		CallbackPath: "/callback", AccountsURL: f.srv.URL, APIURL: f.srv.URL,
	}
	cfg.Key = sha256.Sum256([]byte("test-key-test-key-test-key-test-key"))
	db, err := openDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := newServer(cfg, db)
	srv := httptest.NewUnstartedServer(s.routes())
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	return &harness{t: t, s: s, url: cfg.PublicURL, fake: f,
		client: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (h *harness) do(method, path string, body string, hdr map[string]string) (int, http.Header, []byte) {
	h.t.Helper()
	u := path
	if strings.HasPrefix(path, "/") {
		u = h.url + path
	}
	req, _ := http.NewRequest(method, u, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func (h *harness) dashboard(method, path string, v any) (int, map[string]any) {
	body := ""
	if v != nil {
		b, _ := json.Marshal(v)
		body = string(b)
	}
	code, _, b := h.do(method, "/dashboard/api"+path, body, map[string]string{"Remote-User": "owner", "Content-Type": "application/json"})
	var m map[string]any
	json.Unmarshal(b, &m)
	return code, m
}

func jsonOf(b []byte) map[string]any {
	var m map[string]any
	json.Unmarshal(b, &m)
	return m
}

func pkce() (verifier, challenge string) {
	verifier = randB64(48)
	h := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(h[:])
}

// login drives /authorize -> fake Spotify -> /callback and returns the final
// redirect Location on the client's redirect URI.
func (h *harness) login(clientID, redirect, state, challenge string) (int, *url.URL) {
	q := url.Values{"client_id": {clientID}, "response_type": {"code"}, "redirect_uri": {redirect}, "state": {state}, "scope": {"user-read-private"}}
	if challenge != "" {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	code, hd, _ := h.do("GET", "/authorize?"+q.Encode(), "", nil)
	for i := 0; i < 4 && code == 302; i++ {
		loc := hd.Get("Location")
		if strings.HasPrefix(loc, "https://app.example/") {
			u, _ := url.Parse(loc)
			return code, u
		}
		code, hd, _ = h.do("GET", loc, "", nil)
	}
	return code, nil
}

func TestEndToEnd(t *testing.T) {
	h := newHarness(t)

	// --- dashboard auth: forward-auth header required
	if c, _, _ := h.do("GET", "/dashboard/api/me", "", nil); c != 403 {
		t.Fatalf("dashboard without forward-auth header: got %d want 403", c)
	}
	if c, _, _ := h.do("GET", "/healthz", "", nil); c != 200 {
		t.Fatal("healthz must be public")
	}

	// --- upstream + virtual app
	if c, _ := h.dashboard("PUT", "/upstream", map[string]any{"clientId": "real-client", "clientSecret": "real-secret"}); c != 200 {
		t.Fatalf("save upstream: %d", c)
	}
	c, app := h.dashboard("POST", "/apps", map[string]any{"name": "My app", "redirectUris": []string{"https://app.example/cb"}})
	if c != 201 {
		t.Fatalf("create app: %d %v", c, app)
	}
	cid, secret, appID := app["clientId"].(string), app["clientSecret"].(string), app["id"].(string)
	if c, _ := h.dashboard("POST", "/apps", map[string]any{"name": "x", "redirectUris": []string{"javascript:alert(1)"}}); c != 400 {
		t.Fatal("javascript: redirect URI must be rejected")
	}

	// --- authorize validation
	if c, _, _ := h.do("GET", "/authorize?client_id=nope&response_type=code&redirect_uri=https://app.example/cb", "", nil); c != 400 {
		t.Fatalf("unknown client: %d", c)
	}
	if c, _, _ := h.do("GET", "/authorize?client_id="+cid+"&response_type=code&redirect_uri=https://evil.example/cb", "", nil); c != 400 {
		t.Fatalf("unregistered redirect must not redirect: %d", c)
	}

	// --- PKCE login; wrong verifier burns the code
	ver, ch := pkce()
	_, loc := h.login(cid, "https://app.example/cb", "st1", ch)
	if loc == nil || loc.Query().Get("state") != "st1" || loc.Query().Get("code") == "" {
		t.Fatalf("bad client redirect: %v", loc)
	}
	form := func(v url.Values) (int, map[string]any) {
		c, _, b := h.do("POST", "/api/token", v.Encode(), map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		return c, jsonOf(b)
	}
	if c, m := form(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {"https://app.example/cb"}, "client_id": {cid}, "code_verifier": {"wrong"}}); c != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("wrong verifier: %d %v", c, m)
	}
	_, loc = h.login(cid, "https://app.example/cb", "st2", ch)
	c, tok := form(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {"https://app.example/cb"}, "client_id": {cid}, "code_verifier": {ver}})
	if c != 200 || tok["access_token"] == "" || tok["refresh_token"] == "" || tok["token_type"] != "Bearer" {
		t.Fatalf("token exchange: %d %v", c, tok)
	}
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)
	if c, _ := form(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {"https://app.example/cb"}, "client_id": {cid}, "code_verifier": {ver}}); c != 400 {
		t.Fatal("authorization code must be single use")
	}

	// --- API proxy
	bearer := map[string]string{"Authorization": "Bearer " + access}
	c, _, b := h.do("GET", "/v1/me", "", bearer)
	if c != 200 || jsonOf(b)["id"] != "user1" {
		t.Fatalf("/v1/me: %d %s", c, b)
	}
	if c, _, _ := h.do("GET", "/v1/me", "", map[string]string{"Authorization": "Bearer junk"}); c != 401 {
		t.Fatal("junk token must be 401")
	}
	_, _, b = h.do("GET", "/v1/me/playlists", "", bearer)
	if next, _ := jsonOf(b)["next"].(string); !strings.HasPrefix(next, h.url+"/v1/me/playlists") {
		t.Fatalf("next link not rewritten: %s", b)
	}

	// --- refresh grant (public client / PKCE style, then Basic)
	c, r1 := form(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {cid}})
	if c != 200 || r1["access_token"] == "" || r1["access_token"] == access {
		t.Fatalf("refresh: %d %v", c, r1)
	}
	if c, _, _ := h.do("GET", "/v1/me", "", map[string]string{"Authorization": "Bearer " + r1["access_token"].(string)}); c != 200 {
		t.Fatal("refreshed token must work")
	}

	// --- confidential flow without PKCE, Basic auth
	basic := "Basic " + base64.StdEncoding.EncodeToString([]byte(cid+":"+secret))
	_, loc = h.login(cid, "https://app.example/cb", "st3", "")
	body := url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {"https://app.example/cb"}, "client_id": {cid}}.Encode()
	if c, _, _ := h.do("POST", "/api/token", body, map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); c != 400 {
		t.Fatalf("no PKCE and no secret must fail: %d", c)
	}
	_, loc = h.login(cid, "https://app.example/cb", "st3", "")
	body = url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "redirect_uri": {"https://app.example/cb"}}.Encode()
	if c, _, b := h.do("POST", "/api/token", body, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": basic}); c != 200 {
		t.Fatalf("basic auth code exchange: %d %s", c, b)
	}
	if c, _, _ := h.do("POST", "/api/token", "grant_type=client_credentials", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(cid+":bad"))}); c != 401 {
		t.Fatal("bad secret must be 401 invalid_client")
	}

	// --- client_credentials
	_, _, b = h.do("POST", "/api/token", "grant_type=client_credentials", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": basic})
	cc, _ := jsonOf(b)["access_token"].(string)
	if cc == "" {
		t.Fatalf("client_credentials: %s", b)
	}
	if c, _, b := h.do("GET", "/v1/search?q=hello", "", map[string]string{"Authorization": "Bearer " + cc}); c != 200 || jsonOf(b)["q"] != "hello" {
		t.Fatalf("cc search: %d %s", c, b)
	}

	// --- connected users + revoke
	req := func() []any {
		code, _, b := h.do("GET", "/dashboard/api/apps/"+appID+"/connections", "", map[string]string{"Remote-User": "owner"})
		var l []any
		json.Unmarshal(b, &l)
		_ = code
		return l
	}
	conns := req()
	if len(conns) != 1 || conns[0].(map[string]any)["userId"] != "user1" {
		t.Fatalf("connections (re-login must upsert): %v", conns)
	}
	if c, _ := h.dashboard("DELETE", "/apps/"+appID+"/connections/"+conns[0].(map[string]any)["id"].(string), nil); c != 204 {
		t.Fatalf("revoke: %d", c)
	}
	if c, _, _ := h.do("GET", "/v1/me", "", bearer); c != 401 {
		t.Fatal("revoked user's token must stop working")
	}
	if c, m := form(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {cid}}); c != 400 || m["error"] != "invalid_grant" {
		t.Fatalf("revoked refresh token: %d %v", c, m)
	}

	// --- secret rotation
	c, rot := h.dashboard("POST", "/apps/"+appID+"/rotate-secret", nil)
	if c != 200 || rot["clientSecret"] == secret {
		t.Fatal("rotate must change the secret")
	}
	if c, _, _ := h.do("POST", "/api/token", "grant_type=client_credentials", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": basic}); c != 401 {
		t.Fatal("old secret must stop working")
	}

	// --- Spotify dev-mode allowlist (403 from /v1/me) shows a friendly page
	h.fake.mu.Lock()
	h.fake.blocked = true
	h.fake.mu.Unlock()
	code, _ := h.login(cid, "https://app.example/cb", "st4", ch)
	if code != 403 {
		t.Fatalf("non-allowlisted user: got %d want 403 page", code)
	}

	// --- upstream rate limit is passed through and remembered
	_, _, b = h.do("POST", "/api/token", "grant_type=client_credentials", map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(cid+":"+rot["clientSecret"].(string)))})
	cc = jsonOf(b)["access_token"].(string)
	cch := map[string]string{"Authorization": "Bearer " + cc}
	c, hd, _ := h.do("GET", "/v1/limited", "", cch)
	if c != 429 || hd.Get("Retry-After") != "30" {
		t.Fatalf("429 passthrough: %d %v", c, hd)
	}
	c, hd, _ = h.do("GET", "/v1/search?q=x", "", cch)
	if c != 429 || hd.Get("Retry-After") == "" {
		t.Fatalf("expected local back-off 429, got %d", c)
	}

	// --- usage is recorded
	h.s.close()
	_, usage := h.dashboard("GET", "/usage?hours=1", nil)
	if usage["total"].(float64) < 5 {
		t.Fatalf("usage not recorded: %v", usage["total"])
	}
}
