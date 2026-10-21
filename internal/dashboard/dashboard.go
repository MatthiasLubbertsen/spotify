// Package dashboard serves the JSON API behind /dashboard/api. Authentication is done by
// Tinyauth via Traefik forward-auth; this package only checks the identity headers it forwards.
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"spotis/internal/store"
	"spotis/internal/upstream"
	"spotis/internal/util"
)

type API struct {
	St           *store.Store
	Up           *upstream.Client
	PublicURL    string
	AllowedUsers []string
}

type ctxKey struct{}

func (a *API) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /me", a.me)
	m.HandleFunc("GET /config", a.config)
	m.HandleFunc("GET /upstream", a.getUpstream)
	m.HandleFunc("PUT /upstream", a.putUpstream)
	m.HandleFunc("POST /upstream/test", a.testUpstream)
	m.HandleFunc("GET /apps", a.listApps)
	m.HandleFunc("POST /apps", a.createApp)
	m.HandleFunc("GET /apps/{id}", a.getApp)
	m.HandleFunc("PATCH /apps/{id}", a.patchApp)
	m.HandleFunc("DELETE /apps/{id}", a.deleteApp)
	m.HandleFunc("POST /apps/{id}/rotate-secret", a.rotate)
	m.HandleFunc("GET /grants", a.listGrants)
	m.HandleFunc("DELETE /grants/{id}", a.deleteGrant)
	m.HandleFunc("DELETE /users/{id}", a.deleteUser)
	m.HandleFunc("GET /keys", a.listKeys)
	m.HandleFunc("POST /grants/{id}/keys", a.createKey)
	m.HandleFunc("DELETE /keys/{id}", a.deleteKey)
	m.HandleFunc("GET /usage", a.usage)
	m.HandleFunc("GET /requests", a.requests)
	return a.guard(m)
}

// guard enforces the Tinyauth identity headers, the optional user allowlist and same-origin writes.
func (a *API) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("Remote-User")
		email := r.Header.Get("Remote-Email")
		if user == "" && email == "" {
			jerr(w, 401, "dashboard login required")
			return
		}
		if len(a.AllowedUsers) > 0 && !slices.Contains(a.AllowedUsers, strings.ToLower(user)) && !slices.Contains(a.AllowedUsers, strings.ToLower(email)) {
			jerr(w, 403, "this account may not use the dashboard")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					jerr(w, 403, "cross-origin request blocked")
					return
				}
			} else if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" && sfs != "same-origin" && sfs != "none" {
				jerr(w, 403, "cross-origin request blocked")
				return
			}
		}
		name := user
		if name == "" {
			name = email
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, name)))
	})
}

func jerr(w http.ResponseWriter, status int, msg string) {
	util.WriteJSON(w, status, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		jerr(w, 400, "invalid JSON body")
		return false
	}
	return true
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]string{"user": r.Context().Value(ctxKey{}).(string), "email": r.Header.Get("Remote-Email")})
}

func (a *API) config(w http.ResponseWriter, r *http.Request) {
	util.WriteJSON(w, 200, map[string]any{
		"public_url":       a.PublicURL,
		"redirect_uri":     a.Up.CallbackURL,
		"authorize_url":    a.PublicURL + "/authorize",
		"token_url":        a.PublicURL + "/api/token",
		"api_base":         a.PublicURL + "/v1",
		"upstream_ready":   a.Up.Configured(),
		"cooldown_seconds": a.Up.CooldownLeft(),
	})
}

// ---- upstream settings ----

func (a *API) getUpstream(w http.ResponseWriter, r *http.Request) {
	id := a.St.GetSetting(upstream.SettingClientID)
	util.WriteJSON(w, 200, map[string]any{
		"client_id": id, "has_secret": a.St.GetSetting(upstream.SettingClientSecret) != "",
		"configured": a.Up.Configured(), "redirect_uri": a.Up.CallbackURL,
	})
}

func (a *API) putUpstream(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.ClientID, in.ClientSecret = strings.TrimSpace(in.ClientID), strings.TrimSpace(in.ClientSecret)
	if in.ClientID == "" {
		jerr(w, 400, "client_id is required")
		return
	}
	if err := a.St.SetSetting(upstream.SettingClientID, in.ClientID); err != nil {
		jerr(w, 500, "could not save")
		return
	}
	if in.ClientSecret != "" {
		enc, err := a.Up.Box.Seal(in.ClientSecret)
		if err == nil {
			err = a.St.SetSetting(upstream.SettingClientSecret, enc)
		}
		if err != nil {
			jerr(w, 500, "could not save")
			return
		}
	}
	a.Up.InvalidateCC()
	a.getUpstream(w, r)
}

func (a *API) testUpstream(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	err := a.Up.Test(ctx)
	switch {
	case err == nil:
		util.WriteJSON(w, 200, map[string]any{"ok": true})
	case errors.Is(err, upstream.ErrNotConfigured):
		util.WriteJSON(w, 200, map[string]any{"ok": false, "error": "Client ID and secret are not set yet."})
	default:
		util.WriteJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
	}
}

// ---- apps ----

type appIn struct {
	Name         *string   `json:"name"`
	RedirectURIs *[]string `json:"redirect_uris"`
	Scopes       *[]string `json:"allowed_scopes"`
	RateLimit    *int      `json:"rate_limit_per_min"`
	AllowCC      *bool     `json:"allow_client_credentials"`
	Enabled      *bool     `json:"enabled"`
}

func (in *appIn) apply(a *store.App) string {
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 80 {
			return "name must be 1-80 characters"
		}
		a.Name = n
	}
	if in.RedirectURIs != nil {
		uris := []string{}
		for _, u := range *in.RedirectURIs {
			if u = strings.TrimSpace(u); u == "" {
				continue
			}
			if !util.ValidRedirect(u) {
				return "invalid redirect URI: " + u + " (use https, or http with 127.0.0.1 / [::1])"
			}
			if !slices.Contains(uris, u) {
				uris = append(uris, u)
			}
		}
		a.RedirectURIs = uris
	}
	if in.Scopes != nil {
		a.AllowedScopes = strings.Fields(strings.Join(*in.Scopes, " "))
	}
	if in.RateLimit != nil {
		if *in.RateLimit < 0 {
			return "rate limit must be 0 (unlimited) or more"
		}
		a.RateLimit = *in.RateLimit
	}
	if in.AllowCC != nil {
		a.AllowCC = *in.AllowCC
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	return ""
}

func (a *API) listApps(w http.ResponseWriter, r *http.Request) {
	apps, err := a.St.ListApps()
	if err != nil {
		jerr(w, 500, "could not load apps")
		return
	}
	util.WriteJSON(w, 200, apps)
}

func (a *API) createApp(w http.ResponseWriter, r *http.Request) {
	var in appIn
	if !decode(w, r, &in) {
		return
	}
	if in.Name == nil {
		jerr(w, 400, "name is required")
		return
	}
	app := &store.App{ID: util.RandHex(16), AllowCC: true, Enabled: true, RedirectURIs: []string{}, AllowedScopes: []string{}}
	if msg := in.apply(app); msg != "" {
		jerr(w, 400, msg)
		return
	}
	secret := util.RandHex(16)
	app.SecretHash = util.Hash(secret)
	if err := a.St.CreateApp(app); err != nil {
		jerr(w, 500, "could not create app")
		return
	}
	util.WriteJSON(w, 201, map[string]any{"app": app, "client_secret": secret})
}

func (a *API) app(w http.ResponseWriter, r *http.Request) *store.App {
	app, err := a.St.GetApp(r.PathValue("id"))
	if err != nil {
		jerr(w, 404, "app not found")
		return nil
	}
	return app
}

func (a *API) getApp(w http.ResponseWriter, r *http.Request) {
	if app := a.app(w, r); app != nil {
		util.WriteJSON(w, 200, app)
	}
}

func (a *API) patchApp(w http.ResponseWriter, r *http.Request) {
	app := a.app(w, r)
	if app == nil {
		return
	}
	var in appIn
	if !decode(w, r, &in) {
		return
	}
	if msg := in.apply(app); msg != "" {
		jerr(w, 400, msg)
		return
	}
	if err := a.St.UpdateApp(app); err != nil {
		jerr(w, 500, "could not save")
		return
	}
	util.WriteJSON(w, 200, app)
}

func (a *API) deleteApp(w http.ResponseWriter, r *http.Request) {
	app := a.app(w, r)
	if app == nil {
		return
	}
	gs, _ := a.St.ListGrants(app.ID)
	if err := a.St.DeleteApp(app.ID); err != nil {
		jerr(w, 500, "could not delete")
		return
	}
	for _, g := range gs {
		a.Up.Forget(g.ID)
	}
	w.WriteHeader(204)
}

func (a *API) rotate(w http.ResponseWriter, r *http.Request) {
	app := a.app(w, r)
	if app == nil {
		return
	}
	secret := util.RandHex(16)
	if err := a.St.SetAppSecret(app.ID, util.Hash(secret)); err != nil {
		jerr(w, 500, "could not rotate")
		return
	}
	app, _ = a.St.GetApp(app.ID)
	util.WriteJSON(w, 200, map[string]any{"app": app, "client_secret": secret})
}

// ---- users / grants ----

func (a *API) listGrants(w http.ResponseWriter, r *http.Request) {
	gs, err := a.St.ListGrants(r.URL.Query().Get("app"))
	if err != nil {
		jerr(w, 500, "could not load users")
		return
	}
	util.WriteJSON(w, 200, gs)
}

func (a *API) deleteGrant(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ok, err := a.St.DeleteGrant(id)
	if err != nil || !ok {
		jerr(w, 404, "connection not found")
		return
	}
	a.Up.Forget(id)
	w.WriteHeader(204)
}

func (a *API) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	gs, _ := a.St.ListGrants("")
	ok, err := a.St.DeleteUser(id)
	if err != nil || !ok {
		jerr(w, 404, "user not found")
		return
	}
	for _, g := range gs {
		if g.UserID == id {
			a.Up.Forget(g.ID)
		}
	}
	w.WriteHeader(204)
}

// ---- usage ----

func (a *API) usage(w http.ResponseWriter, r *http.Request) {
	window, bucket := 24*time.Hour, time.Hour
	switch r.URL.Query().Get("range") {
	case "1h":
		window, bucket = time.Hour, 5*time.Minute
	case "7d":
		window, bucket = 7*24*time.Hour, 6*time.Hour
	case "30d":
		window, bucket = 30*24*time.Hour, 24*time.Hour
	}
	u, err := a.St.GetUsage(r.URL.Query().Get("app"), window, bucket)
	if err != nil {
		jerr(w, 500, "could not load usage")
		return
	}
	util.WriteJSON(w, 200, u)
}

func (a *API) requests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 500 {
		limit = 100
	}
	rs, err := a.St.ListRequests(q.Get("app"), limit, q.Get("errors") == "1")
	if err != nil {
		jerr(w, 500, "could not load requests")
		return
	}
	util.WriteJSON(w, 200, rs)
}

// ---- now-playing API keys ----

func (a *API) listKeys(w http.ResponseWriter, r *http.Request) {
	ks, err := a.St.ListAPIKeys()
	if err != nil {
		jerr(w, 500, "could not load keys")
		return
	}
	util.WriteJSON(w, 200, ks)
}

func (a *API) createKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if _, err := a.St.GetGrant(id); err != nil {
		jerr(w, 404, "connection not found")
		return
	}
	var in struct {
		Label string `json:"label"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&in)
	if len(in.Label) > 80 {
		in.Label = in.Label[:80]
	}
	key := "spk_" + util.RandToken(24)
	k, err := a.St.CreateAPIKey(id, strings.TrimSpace(in.Label), util.Hash(key), key[:8])
	if err != nil {
		jerr(w, 500, "could not create key")
		return
	}
	util.WriteJSON(w, 201, map[string]any{"key_info": k, "key": key})
}

func (a *API) deleteKey(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if ok, err := a.St.DeleteAPIKey(id); err != nil || !ok {
		jerr(w, 404, "key not found")
		return
	}
	w.WriteHeader(204)
}
