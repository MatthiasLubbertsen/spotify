// Package server wires every handler onto one mux.
//
// Route map (see AGENTS.md §3):
//
//	public, protected by client auth/tokens: /authorize, /api/token, /oauth/callback, /v1/*
//	public static: /, /assets/*, /healthz
//	dashboard (Tinyauth forward-auth at Traefik): /dashboard, /dashboard/*, /dashboard/api/*
package server

import (
	"net/http"
	"strings"

	"spotis/internal/config"
	"spotis/internal/crypto"
	"spotis/internal/dashboard"
	"spotis/internal/oauth"
	"spotis/internal/proxy"
	"spotis/internal/store"
	"spotis/internal/upstream"
	"spotis/internal/usage"
	"spotis/internal/webui"
)

func New(cfg config.Config, st *store.Store, box *crypto.Box) http.Handler {
	up := upstream.New(st, box, cfg.AccountsURL, cfg.APIURL, cfg.PublicURL)
	lg := usage.NewLogger(st)
	oa := &oauth.Handler{St: st, Up: up}
	px := proxy.New(st, up, cfg.PublicURL)
	dash := &dashboard.API{St: st, Up: up, PublicURL: cfg.PublicURL, AllowedUsers: cfg.AllowedUsers}
	ui := webui.Handler()

	mux := http.NewServeMux()
	mux.Handle("GET /authorize", lg.Wrap("oauth", http.HandlerFunc(oa.Authorize)))
	mux.Handle("GET /oauth/callback", lg.Wrap("oauth", http.HandlerFunc(oa.Callback)))
	mux.Handle("POST /api/token", lg.Wrap("oauth", http.HandlerFunc(oa.Token)))
	mux.Handle("/v1/", lg.Wrap("api", px))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("/dashboard/api/", http.StripPrefix("/dashboard/api", noStore(dash.Handler())))
	mux.Handle("/dashboard/", requireIdentity(cfg.AllowedUsers, ui))
	mux.Handle("/dashboard", requireIdentity(cfg.AllowedUsers, ui))
	mux.Handle("/", ui)
	return secureHeaders(mux)
}

// requireIdentity is a second line of defence behind Traefik: no forwarded identity, no dashboard HTML.
func requireIdentity(_ []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Remote-User") == "" && r.Header.Get("Remote-Email") == "" {
			http.Error(w, "dashboard login required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
		}
		next.ServeHTTP(w, r)
	})
}
