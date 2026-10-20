package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	accessTTL   = 3600
	codeTTL     = 600
	pendingTTL  = 600
	flowCookie  = "sp_flow"
	tokenPrefix = "spx_"
	refrPrefix  = "spr_"
)

type App struct {
	ID, Name, ClientID string
	SecretEnc          string
	RedirectURIs       []string
	AllowedScopes      string
	RateLimit          int
	Enabled            bool
	CreatedAt          int64
}

const appCols = `id,name,client_id,secret_enc,redirect_uris,allowed_scopes,rate_limit,enabled,created_at`

func scanApp(sc interface{ Scan(...any) error }) (*App, error) {
	var a App
	var uris string
	var en int
	if err := sc.Scan(&a.ID, &a.Name, &a.ClientID, &a.SecretEnc, &uris, &a.AllowedScopes, &a.RateLimit, &en, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.Enabled = en == 1
	json.Unmarshal([]byte(uris), &a.RedirectURIs)
	return &a, nil
}

func (s *Server) appByClientID(cid string) *App {
	a, err := scanApp(s.db.QueryRow(`SELECT `+appCols+` FROM apps WHERE client_id=?`, cid))
	if err != nil {
		return nil
	}
	return a
}

func (s *Server) appByID(id string) *App {
	a, err := scanApp(s.db.QueryRow(`SELECT `+appCols+` FROM apps WHERE id=?`, id))
	if err != nil {
		return nil
	}
	return a
}

func (a *App) hasRedirect(u string) bool {
	for _, r := range a.RedirectURIs {
		if r == u {
			return true
		}
	}
	return false
}

// ---------------- /authorize ----------------

// handleAuthorize mimics accounts.spotify.com/authorize: validate the virtual
// app, remember the request, then send the browser to the real Spotify with
// our own PKCE pair and the single fixed redirect URI.
func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	q := r.URL.Query()
	app := s.appByClientID(q.Get("client_id"))
	appID := ""
	if app != nil {
		appID = app.ID
	}
	defer func() { s.logReq(appID, "", "GET", "/authorize", sw.status, started) }()

	if app == nil || !app.Enabled {
		errPage(w, 400, "Invalid client_id", "This client_id does not belong to an enabled app on this server.")
		return
	}
	redirect := q.Get("redirect_uri")
	if !app.hasRedirect(redirect) {
		errPage(w, 400, "Invalid redirect_uri", "The redirect_uri must exactly match one registered for this app in the dashboard (case and trailing slashes matter).")
		return
	}
	state := q.Get("state")
	fail := func(code, desc string) {
		redirectClient(w, r, redirect, state, url.Values{"error": {code}, "error_description": {desc}})
	}

	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "response_type must be code")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge != "" {
		if q.Get("code_challenge_method") != "S256" {
			fail("invalid_request", "code_challenge_method must be S256")
			return
		}
		if len(challenge) != 43 {
			fail("invalid_request", "invalid code_challenge")
			return
		}
	}
	scope := strings.Join(splitScopes(q.Get("scope")), " ")
	if app.AllowedScopes != "" {
		allowed := map[string]bool{}
		for _, sc := range splitScopes(app.AllowedScopes) {
			allowed[sc] = true
		}
		for _, sc := range splitScopes(scope) {
			if !allowed[sc] {
				fail("invalid_scope", "scope "+sc+" is not allowed for this app")
				return
			}
		}
	}
	upID, _ := s.upstreamCreds()
	if upID == "" {
		errPage(w, 503, "Upstream not configured", "The Spotify app client ID has not been set in the dashboard yet.")
		return
	}

	id := randB64(24)
	verifier := randB64(48) // 64 chars
	_, err := s.db.Exec(`INSERT INTO pending(id,app_id,redirect_uri,state,scope,challenge,verifier_enc,expires) VALUES(?,?,?,?,?,?,?,?)`,
		id, app.ID, redirect, state, scope, challenge, s.seal(verifier), time.Now().Unix()+pendingTTL)
	if err != nil {
		errPage(w, 500, "Server error", "Could not start the login.")
		return
	}
	h := sha256.Sum256([]byte(verifier))
	up := url.Values{
		"client_id":             {upID},
		"response_type":         {"code"},
		"redirect_uri":          {s.callbackURL()},
		"state":                 {id},
		"code_challenge_method": {"S256"},
		"code_challenge":        {b64.EncodeToString(h[:])},
	}
	if scope != "" {
		up.Set("scope", scope)
	}
	if q.Get("show_dialog") == "true" {
		up.Set("show_dialog", "true")
	}
	// Binds the flow to this browser (login-CSRF protection), checked in /callback.
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: id, Path: "/", MaxAge: pendingTTL, HttpOnly: true, Secure: strings.HasPrefix(s.cfg.PublicURL, "https"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.cfg.AccountsURL+"/authorize?"+up.Encode(), http.StatusFound)
}

func redirectClient(w http.ResponseWriter, r *http.Request, redirect, state string, extra url.Values) {
	u, err := url.Parse(redirect)
	if err != nil {
		errPage(w, 400, "Invalid redirect_uri", "")
		return
	}
	q := u.Query()
	for k, v := range extra {
		q.Set(k, v[0])
	}
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func errPage(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><title>%s</title>
<body style="font-family:system-ui;background:#09090b;color:#fafafa;display:grid;place-items:center;min-height:100vh;margin:0">
<div style="max-width:32rem;padding:1.5rem"><h1 style="font-size:1.25rem">%s</h1><p style="color:#a1a1aa;line-height:1.5">%s</p></div>`,
		html.EscapeString(title), html.EscapeString(title), msg)
}

// ---------------- /callback ----------------

type upToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type upError struct {
	Status     int
	Code       string
	Desc       string
	RetryAfter string
}

func (e *upError) Error() string { return fmt.Sprintf("upstream %d %s %s", e.Status, e.Code, e.Desc) }

// upstreamToken POSTs to Spotify's token endpoint. basic=true uses the real
// app's client secret (client_credentials); otherwise PKCE (client_id in body).
func (s *Server) upstreamToken(form url.Values, basic bool) (*upToken, error) {
	id, secret := s.upstreamCreds()
	req, _ := http.NewRequest("POST", s.cfg.AccountsURL+"/api/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basic {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(id+":"+secret)))
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, &upError{Status: 502, Code: "server_error", Desc: err.Error()}
	}
	defer resp.Body.Close()
	var t upToken
	json.NewDecoder(resp.Body).Decode(&t)
	if resp.StatusCode != 200 || t.AccessToken == "" {
		if resp.StatusCode == 429 {
			s.noteRateLimit(resp.Header.Get("Retry-After"))
		}
		return nil, &upError{Status: resp.StatusCode, Code: t.Error, Desc: t.ErrorDesc, RetryAfter: resp.Header.Get("Retry-After")}
	}
	return &t, nil
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	q := r.URL.Query()
	id := q.Get("state")
	var p struct {
		appID, redirect, state, scope, challenge, verifierEnc string
		expires                                               int64
	}
	err := s.db.QueryRow(`DELETE FROM pending WHERE id=? RETURNING app_id,redirect_uri,state,scope,challenge,verifier_enc,expires`, id).
		Scan(&p.appID, &p.redirect, &p.state, &p.scope, &p.challenge, &p.verifierEnc, &p.expires)
	defer func() { s.logReq(p.appID, "", "GET", s.cfg.CallbackPath, sw.status, started) }()
	if err != nil || p.expires < time.Now().Unix() {
		errPage(w, 400, "Login expired", "This login link is no longer valid. Go back to the app and start again.")
		return
	}
	if c, err := r.Cookie(flowCookie); err != nil || c.Value != id {
		errPage(w, 400, "Browser mismatch", "This login was started in a different browser. Go back to the app and start again.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: "/", MaxAge: -1})

	if e := q.Get("error"); e != "" {
		redirectClient(w, r, p.redirect, p.state, url.Values{"error": {e}})
		return
	}
	verifier, _ := s.open(p.verifierEnc)
	upID, _ := s.upstreamCreds()
	tok, err := s.upstreamToken(url.Values{
		"grant_type": {"authorization_code"}, "code": {q.Get("code")}, "redirect_uri": {s.callbackURL()},
		"client_id": {upID}, "code_verifier": {verifier},
	}, false)
	if err != nil {
		redirectClient(w, r, p.redirect, p.state, url.Values{"error": {"server_error"}, "error_description": {"upstream token exchange failed"}})
		return
	}
	// Identify the Spotify user. 403 here is Spotify's development-mode allowlist.
	me, status := s.fetchMe(tok.AccessToken)
	if me == nil {
		if status == 403 {
			errPage(w, 403, "Not allowed by Spotify",
				"Spotify only lets users that are added to the real Spotify app's allowlist (Developer Dashboard &rarr; Users Management) sign in. Ask the owner of this server to add your Spotify account email, then try again.")
			return
		}
		redirectClient(w, r, p.redirect, p.state, url.Values{"error": {"server_error"}, "error_description": {"could not read Spotify profile"}})
		return
	}
	connID := randHex(8)
	now := time.Now().Unix()
	err = s.db.QueryRow(`INSERT INTO connections(id,app_id,user_id,display_name,scope,refresh_enc,access_enc,access_exp,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(app_id,user_id) DO UPDATE SET display_name=excluded.display_name, scope=excluded.scope,
		  refresh_enc=excluded.refresh_enc, access_enc=excluded.access_enc, access_exp=excluded.access_exp
		RETURNING id`,
		connID, p.appID, me.ID, me.DisplayName, tok.Scope, s.seal(tok.RefreshToken), s.seal(tok.AccessToken), now+int64(tok.ExpiresIn), now).Scan(&connID)
	if err != nil {
		errPage(w, 500, "Server error", "Could not save the connection.")
		return
	}
	code := randB64(32)
	s.db.Exec(`INSERT INTO codes(hash,app_id,connection_id,redirect_uri,challenge,expires) VALUES(?,?,?,?,?,?)`,
		hashToken(code), p.appID, connID, p.redirect, p.challenge, now+codeTTL)
	redirectClient(w, r, p.redirect, p.state, url.Values{"code": {code}})
}

type spotifyMe struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

func (s *Server) fetchMe(access string) (*spotifyMe, int) {
	req, _ := http.NewRequest("GET", s.cfg.APIURL+"/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, 502
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		s.noteRateLimit(resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode != 200 {
		return nil, resp.StatusCode
	}
	var m spotifyMe
	json.NewDecoder(resp.Body).Decode(&m)
	if m.ID == "" {
		return nil, 502
	}
	return &m, 200
}

// ---------------- /api/token ----------------

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	appID := ""
	defer func() { s.logReq(appID, "", "POST", "/api/token", sw.status, started) }()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		oauthErr(w, 400, "invalid_request", "malformed form body")
		return
	}
	app, secretOK, errCode := s.authClient(r)
	if app != nil {
		appID = app.ID
	}
	if errCode != "" {
		if r.Header.Get("Authorization") != "" {
			w.Header().Set("WWW-Authenticate", `Basic realm="spotify"`)
		}
		oauthErr(w, 401, "invalid_client", errCode)
		return
	}

	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		s.grantCode(w, r, app, secretOK)
	case "refresh_token":
		s.grantRefresh(w, r, app)
	case "client_credentials":
		if !secretOK {
			oauthErr(w, 401, "invalid_client", "client_credentials requires the client secret")
			return
		}
		if _, sec := s.upstreamCreds(); sec == "" {
			oauthErr(w, 400, "unsupported_grant_type", "client_credentials is unavailable: the upstream client secret is not set in the dashboard")
			return
		}
		access := s.newToken("access", app.ID, "", "", accessTTL)
		cors(w)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": accessTTL})
	default:
		oauthErr(w, 400, "unsupported_grant_type", "grant_type must be authorization_code, refresh_token or client_credentials")
	}
}

// authClient accepts Basic auth or client_id[/client_secret] in the body, as
// Spotify does. secretOK reports whether a valid secret was presented.
func (s *Server) authClient(r *http.Request) (app *App, secretOK bool, errDesc string) {
	cid, secret := r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	if u, p, ok := r.BasicAuth(); ok {
		cid, secret = u, p
	}
	if cid == "" {
		return nil, false, "Missing client_id"
	}
	app = s.appByClientID(cid)
	if app == nil || !app.Enabled {
		return nil, false, "Invalid client"
	}
	if secret != "" {
		real, _ := s.open(app.SecretEnc)
		if subtle.ConstantTimeCompare([]byte(secret), []byte(real)) != 1 {
			return app, false, "Invalid client secret"
		}
		return app, true, ""
	}
	return app, false, ""
}

func (s *Server) grantCode(w http.ResponseWriter, r *http.Request, app *App, secretOK bool) {
	code := r.PostForm.Get("code")
	var appID, connID, redirect, challenge string
	var expires int64
	err := s.db.QueryRow(`DELETE FROM codes WHERE hash=? RETURNING app_id,connection_id,redirect_uri,challenge,expires`, hashToken(code)).
		Scan(&appID, &connID, &redirect, &challenge, &expires)
	if err != nil || expires < time.Now().Unix() || appID != app.ID {
		oauthErr(w, 400, "invalid_grant", "Invalid authorization code")
		return
	}
	if r.PostForm.Get("redirect_uri") != redirect {
		oauthErr(w, 400, "invalid_grant", "Invalid redirect URI")
		return
	}
	if challenge != "" {
		v := r.PostForm.Get("code_verifier")
		h := sha256.Sum256([]byte(v))
		if v == "" || subtle.ConstantTimeCompare([]byte(b64.EncodeToString(h[:])), []byte(challenge)) != 1 {
			oauthErr(w, 400, "invalid_grant", "code_verifier was incorrect")
			return
		}
	} else if !secretOK {
		oauthErr(w, 400, "invalid_request", "code_verifier required (PKCE) or authenticate with the client secret")
		return
	}
	var scope string
	if err := s.db.QueryRow(`SELECT scope FROM connections WHERE id=?`, connID).Scan(&scope); err != nil {
		oauthErr(w, 400, "invalid_grant", "Invalid authorization code")
		return
	}
	access := s.newToken("access", app.ID, connID, scope, accessTTL)
	refresh := s.newToken("refresh", app.ID, connID, scope, 0)
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "scope": scope, "expires_in": accessTTL, "refresh_token": refresh})
}

func (s *Server) grantRefresh(w http.ResponseWriter, r *http.Request, app *App) {
	rt := r.PostForm.Get("refresh_token")
	var appID, connID, scope string
	err := s.db.QueryRow(`SELECT t.app_id, t.connection_id, c.scope FROM tokens t JOIN connections c ON c.id=t.connection_id
		WHERE t.hash=? AND t.kind='refresh'`, hashToken(rt)).Scan(&appID, &connID, &scope)
	if err != nil || appID != app.ID {
		oauthErr(w, 400, "invalid_grant", "Invalid refresh token")
		return
	}
	access := s.newToken("access", app.ID, connID, scope, accessTTL)
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	// Spotify may omit refresh_token; we echo the same one, which every client handles.
	writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "scope": scope, "expires_in": accessTTL, "refresh_token": rt})
}

// newToken stores only the hash and returns the plaintext once.
func (s *Server) newToken(kind, appID, connID, scope string, ttl int) string {
	prefix := tokenPrefix
	if kind == "refresh" {
		prefix = refrPrefix
	}
	tok := prefix + randB64(32)
	var exp int64
	if ttl > 0 {
		exp = time.Now().Unix() + int64(ttl)
	}
	var conn any
	if connID != "" {
		conn = connID
	}
	s.db.Exec(`INSERT INTO tokens(hash,kind,app_id,connection_id,scope,expires) VALUES(?,?,?,?,?,?)`, hashToken(tok), kind, appID, conn, scope, exp)
	return tok
}

// ---------------- upstream access tokens ----------------

var errInvalidGrant = errors.New("upstream grant revoked")

// upstreamAccess returns a valid real-Spotify access token for a connection,
// refreshing it (serialised per connection) when it is about to expire.
func (s *Server) upstreamAccess(connID string, force bool) (string, error) {
	mu, _ := s.connLocks.LoadOrStore(connID, new(sync.Mutex))
	m := mu.(*sync.Mutex)
	m.Lock()
	defer m.Unlock()

	var accEnc, refEnc string
	var exp int64
	if err := s.db.QueryRow(`SELECT access_enc,refresh_enc,access_exp FROM connections WHERE id=?`, connID).Scan(&accEnc, &refEnc, &exp); err != nil {
		return "", errInvalidGrant
	}
	if !force && exp > time.Now().Unix()+60 {
		if a, err := s.open(accEnc); err == nil && a != "" {
			return a, nil
		}
	}
	refresh, err := s.open(refEnc)
	if err != nil {
		return "", err
	}
	upID, _ := s.upstreamCreds()
	tok, err := s.upstreamToken(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {upID}}, false)
	if err != nil {
		var ue *upError
		if errors.As(err, &ue) && ue.Code == "invalid_grant" {
			s.db.Exec(`DELETE FROM connections WHERE id=?`, connID) // cascades our tokens
			return "", errInvalidGrant
		}
		return "", err
	}
	newRefEnc := refEnc
	if tok.RefreshToken != "" {
		newRefEnc = s.seal(tok.RefreshToken)
	}
	s.db.Exec(`UPDATE connections SET access_enc=?, access_exp=?, refresh_enc=? WHERE id=?`,
		s.seal(tok.AccessToken), time.Now().Unix()+int64(tok.ExpiresIn), newRefEnc, connID)
	return tok.AccessToken, nil
}

// upstreamAppToken is the real app's client_credentials token (no user).
func (s *Server) upstreamAppToken(force bool) (string, error) {
	s.ccMu.Lock()
	defer s.ccMu.Unlock()
	if !force && s.ccToken != "" && time.Now().Before(s.ccExpires.Add(-60*time.Second)) {
		return s.ccToken, nil
	}
	tok, err := s.upstreamToken(url.Values{"grant_type": {"client_credentials"}}, true)
	if err != nil {
		return "", err
	}
	s.ccToken, s.ccExpires = tok.AccessToken, time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second)
	return s.ccToken, nil
}

// noteRateLimit records Spotify's Retry-After so we stop hammering upstream.
func (s *Server) noteRateLimit(retryAfter string) {
	n, err := strconv.Atoi(strings.TrimSpace(retryAfter))
	if err != nil || n <= 0 {
		n = 1
	}
	if n > 86400 {
		n = 86400
	}
	s.blkMu.Lock()
	if t := time.Now().Add(time.Duration(n) * time.Second); t.After(s.blkTil) {
		s.blkTil = t
	}
	s.blkMu.Unlock()
}

func (s *Server) blockedFor() time.Duration {
	s.blkMu.Lock()
	defer s.blkMu.Unlock()
	if d := time.Until(s.blkTil); d > 0 {
		return d
	}
	return 0
}
