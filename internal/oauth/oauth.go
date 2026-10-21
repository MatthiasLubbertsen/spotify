// Package oauth mimics Spotify's accounts service for virtual apps:
// GET /authorize, GET /oauth/callback (upstream redirect target) and POST /api/token.
package oauth

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"spotis/internal/store"
	"spotis/internal/upstream"
	"spotis/internal/usage"
	"spotis/internal/util"
)

const (
	accessTTL   = 3600
	authReqTTL  = 10 * 60
	authCodeTTL = 5 * 60
)

type Handler struct {
	St *store.Store
	Up *upstream.Client
}

var errPage = template.Must(template.New("e").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title><style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#09090b;color:#fafafa;font:16px/1.5 system-ui,sans-serif}
main{max-width:30rem;padding:2rem}h1{font-size:1.25rem;margin:0 0 .5rem}p{color:#a1a1aa;margin:.5rem 0}a{color:#fafafa}
code{background:#27272a;padding:.1rem .35rem;border-radius:.25rem}</style>
<main><h1>{{.Title}}</h1><p>{{.Body}}</p>{{if .Back}}<p><a href="{{.Back}}">Return to the app</a></p>{{end}}</main></html>`))

func page(w http.ResponseWriter, status int, title, body, back string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = errPage.Execute(w, map[string]any{"Title": title, "Body": body, "Back": template.URL(back)})
}

// redirectWith sends the user back to the client with query params appended.
func redirectWith(w http.ResponseWriter, r *http.Request, base string, params map[string]string) {
	u, err := url.Parse(base)
	if err != nil {
		page(w, 400, "Invalid redirect URI", "The redirect URI is invalid.", "")
		return
	}
	q := u.Query()
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// Authorize starts the flow: validate the virtual app, then bounce to Spotify.
func (h *Handler) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	app, err := h.St.GetApp(q.Get("client_id"))
	if err != nil || !app.Enabled {
		page(w, 400, "Invalid client", "INVALID_CLIENT: Invalid client", "")
		return
	}
	usage.From(r.Context()).AppID = app.ID
	redirect, state := q.Get("redirect_uri"), q.Get("state")
	if !util.MatchRedirect(app.RedirectURIs, redirect) {
		page(w, 400, "Invalid redirect URI", "INVALID_CLIENT: Invalid redirect URI", "")
		return
	}
	fail := func(code, desc string) {
		redirectWith(w, r, redirect, map[string]string{"error": code, "error_description": desc, "state": state})
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "Only response_type=code is supported")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge != "" && q.Get("code_challenge_method") != "S256" {
		fail("invalid_request", "code_challenge_method must be S256")
		return
	}
	scope := strings.Join(strings.Fields(q.Get("scope")), " ")
	if len(app.AllowedScopes) > 0 {
		allowed := map[string]bool{}
		for _, s := range app.AllowedScopes {
			allowed[s] = true
		}
		for _, s := range strings.Fields(scope) {
			if !allowed[s] {
				fail("invalid_scope", "Scope "+s+" is not enabled for this app")
				return
			}
		}
	}
	id := util.RandToken(24)
	if err := h.St.InsertAuthRequest(store.AuthRequest{ID: id, AppID: app.ID, RedirectURI: redirect, Scope: scope, State: state,
		CodeChallenge: challenge, ExpiresAt: time.Now().Unix() + authReqTTL}); err != nil {
		fail("server_error", "Could not start authorization")
		return
	}
	dest, err := h.Up.AuthorizeURL(id, scope, q.Get("show_dialog") == "true")
	if errors.Is(err, upstream.ErrNotConfigured) {
		fail("server_error", "The upstream Spotify app is not configured yet")
		return
	} else if err != nil {
		fail("server_error", "Could not start authorization")
		return
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

// Callback is the single redirect URI registered with Spotify.
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ar, err := h.St.TakeAuthRequest(q.Get("state"))
	if err != nil {
		page(w, 400, "Login expired", "This login link is no longer valid. Go back to the app and try again.", "")
		return
	}
	usage.From(r.Context()).AppID = ar.AppID
	fail := func(code, desc string) {
		redirectWith(w, r, ar.RedirectURI, map[string]string{"error": code, "error_description": desc, "state": ar.State})
	}
	if e := q.Get("error"); e != "" {
		fail(e, q.Get("error_description"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tok, err := h.Up.Exchange(ctx, q.Get("code"))
	if err != nil || tok.RefreshToken == "" {
		fail("server_error", "Could not complete the Spotify login")
		return
	}
	spID, name, err := h.Up.Me(ctx, tok.AccessToken)
	if errors.Is(err, upstream.ErrForbidden) {
		back := ar.RedirectURI
		if u, e := url.Parse(back); e == nil {
			qq := u.Query()
			qq.Set("error", "access_denied")
			qq.Set("error_description", "Spotify account is not allowlisted for this app")
			qq.Set("state", ar.State)
			u.RawQuery = qq.Encode()
			back = u.String()
		}
		page(w, 403, "Spotify account not allowed",
			"Spotify only lets accounts that the owner has added under Users management in the Spotify Developer Portal sign in to this app. Ask the owner to add your Spotify email, then try again.", back)
		return
	} else if err != nil {
		fail("server_error", "Could not read the Spotify profile")
		return
	}
	enc, err := h.Up.Box.Seal(tok.RefreshToken)
	if err != nil {
		fail("server_error", "Internal error")
		return
	}
	g, err := h.St.UpsertGrant(ar.AppID, spID, name, tok.Scope, enc)
	if err != nil {
		fail("server_error", "Internal error")
		return
	}
	h.Up.Prime(g.ID, tok)
	code := util.RandToken(32)
	if err := h.St.InsertAuthCode(store.AuthCode{Hash: util.Hash(code), AppID: ar.AppID, GrantID: g.ID, RedirectURI: ar.RedirectURI,
		CodeChallenge: ar.CodeChallenge, ExpiresAt: time.Now().Unix() + authCodeTTL}); err != nil {
		fail("server_error", "Internal error")
		return
	}
	redirectWith(w, r, ar.RedirectURI, map[string]string{"code": code, "state": ar.State})
}

func tokenError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Basic realm="spotis"`)
	}
	util.WriteJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// Token implements POST /api/token for authorization_code, refresh_token and client_credentials.
func (h *Handler) Token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		tokenError(w, 400, "invalid_request", "Malformed request body")
		return
	}
	clientID, secret, basic := r.BasicAuth()
	if !basic {
		clientID, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	app, err := h.St.GetApp(clientID)
	if err != nil || !app.Enabled {
		tokenError(w, 401, "invalid_client", "Invalid client")
		return
	}
	usage.From(r.Context()).AppID = app.ID
	hasSecret := secret != ""
	if hasSecret && !util.HashEqual(app.SecretHash, secret) {
		tokenError(w, 401, "invalid_client", "Invalid client secret")
		return
	}
	switch gt := r.PostForm.Get("grant_type"); gt {
	case "authorization_code":
		h.codeGrant(w, r, app, hasSecret)
	case "refresh_token":
		h.refreshGrant(w, r, app)
	case "client_credentials":
		if !hasSecret || !app.AllowCC {
			tokenError(w, 401, "invalid_client", "Client credentials grant is not available for this client")
			return
		}
		access := util.RandToken(32)
		_ = h.St.InsertToken(store.Token{Hash: util.Hash(access), Kind: "access", AppID: app.ID, ExpiresAt: time.Now().Unix() + accessTTL})
		writeTokens(w, access, "", "")
	default:
		tokenError(w, 400, "unsupported_grant_type", "grant_type must be authorization_code, refresh_token or client_credentials")
	}
}

func writeTokens(w http.ResponseWriter, access, refresh, scope string) {
	w.Header().Set("Cache-Control", "no-store")
	resp := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": accessTTL, "scope": scope}
	if refresh != "" {
		resp["refresh_token"] = refresh
	}
	util.WriteJSON(w, 200, resp)
}

func (h *Handler) issue(grantID int64, appID, scope string) (access, refresh string) {
	access = util.RandToken(32)
	_ = h.St.InsertToken(store.Token{Hash: util.Hash(access), Kind: "access", AppID: appID, GrantID: grantID, Scope: scope, ExpiresAt: time.Now().Unix() + accessTTL})
	refresh = util.RandToken(32)
	_ = h.St.InsertToken(store.Token{Hash: util.Hash(refresh), Kind: "refresh", AppID: appID, GrantID: grantID, Scope: scope})
	return
}

func (h *Handler) codeGrant(w http.ResponseWriter, r *http.Request, app *store.App, hasSecret bool) {
	c, err := h.St.TakeAuthCode(util.Hash(r.PostForm.Get("code")))
	if err != nil || c.AppID != app.ID {
		tokenError(w, 400, "invalid_grant", "Invalid authorization code")
		return
	}
	if c.RedirectURI != r.PostForm.Get("redirect_uri") {
		tokenError(w, 400, "invalid_grant", "Invalid redirect URI")
		return
	}
	if c.CodeChallenge != "" {
		v := r.PostForm.Get("code_verifier")
		if v == "" || util.PKCEChallenge(v) != c.CodeChallenge {
			tokenError(w, 400, "invalid_grant", "code_verifier was incorrect")
			return
		}
	} else if !hasSecret {
		tokenError(w, 401, "invalid_client", "Client secret required (or use PKCE)")
		return
	}
	g, err := h.St.GetGrant(c.GrantID)
	if err != nil {
		tokenError(w, 400, "invalid_grant", "Authorization was revoked")
		return
	}
	usage.From(r.Context()).GrantID = g.ID
	access, refresh := h.issue(g.ID, app.ID, g.Scope)
	writeTokens(w, access, refresh, g.Scope)
}

func (h *Handler) refreshGrant(w http.ResponseWriter, r *http.Request, app *store.App) {
	rt := r.PostForm.Get("refresh_token")
	t, err := h.St.GetToken(util.Hash(rt), "refresh")
	if err != nil || t.AppID != app.ID || t.GrantID == 0 {
		tokenError(w, 400, "invalid_grant", "Invalid refresh token")
		return
	}
	// Surface a dead upstream authorization now rather than on the first API call.
	if _, err := h.Up.AccessForGrant(r.Context(), t.GrantID); err != nil {
		if errors.Is(err, upstream.ErrReauth) {
			tokenError(w, 400, "invalid_grant", "Refresh token revoked")
		} else {
			tokenError(w, 503, "temporarily_unavailable", fmt.Sprint("upstream error: ", err))
		}
		return
	}
	usage.From(r.Context()).GrantID = t.GrantID
	access := util.RandToken(32)
	_ = h.St.InsertToken(store.Token{Hash: util.Hash(access), Kind: "access", AppID: app.ID, GrantID: t.GrantID, Scope: t.Scope, ExpiresAt: time.Now().Unix() + accessTTL})
	writeTokens(w, access, rt, t.Scope)
}
