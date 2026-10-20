package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// dash guards everything under /dashboard. Real authentication is Traefik
// forward-auth (Tinyauth); here we only require that it ran (the user header is
// present), optionally restrict to DASHBOARD_ALLOWED_USERS, and apply CSRF checks.
func (s *Server) dash(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := strings.TrimSpace(r.Header.Get(s.cfg.UserHeader))
		if user == "" || (len(s.cfg.AllowedUsers) > 0 && !s.cfg.AllowedUsers[user]) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "dashboard: not authenticated (missing "+s.cfg.UserHeader+" from forward-auth)", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				if ou, err := url.Parse(o); err != nil || ou.Host != r.Host {
					http.Error(w, "bad origin", http.StatusForbidden)
					return
				}
			}
			if strings.HasPrefix(r.URL.Path, "/dashboard/api/") && r.ContentLength != 0 &&
				!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next(w, r)
	}
}

func decode(r *http.Request, v any) bool {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v) == nil
}

func bad(w http.ResponseWriter, msg string) { writeJSON(w, 400, map[string]string{"error": msg}) }

func (s *Server) dashboardAPI(mux *http.ServeMux) {
	h := func(pattern string, f http.HandlerFunc) { mux.HandleFunc(pattern, s.dash(f)) }
	h("GET /dashboard/api/me", s.apiMe)
	h("GET /dashboard/api/upstream", s.apiUpstream)
	h("PUT /dashboard/api/upstream", s.apiUpstreamSave)
	h("GET /dashboard/api/apps", s.apiApps)
	h("POST /dashboard/api/apps", s.apiAppCreate)
	h("GET /dashboard/api/apps/{id}", s.apiAppGet)
	h("PATCH /dashboard/api/apps/{id}", s.apiAppUpdate)
	h("DELETE /dashboard/api/apps/{id}", s.apiAppDelete)
	h("POST /dashboard/api/apps/{id}/rotate-secret", s.apiAppRotate)
	h("GET /dashboard/api/apps/{id}/connections", s.apiConnections)
	h("DELETE /dashboard/api/apps/{id}/connections", s.apiConnectionsRevokeAll)
	h("DELETE /dashboard/api/apps/{id}/connections/{cid}", s.apiConnectionRevoke)
	h("GET /dashboard/api/usage", s.apiUsage)
	h("GET /dashboard/api/requests", s.apiRequests)
}

func (s *Server) apiMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"user":         r.Header.Get(s.cfg.UserHeader),
		"publicUrl":    s.cfg.PublicURL,
		"authorizeUrl": s.cfg.PublicURL + "/authorize",
		"tokenUrl":     s.cfg.PublicURL + "/api/token",
		"apiUrl":       s.cfg.PublicURL + "/v1",
		"callbackUrl":  s.callbackURL(),
	})
}

// ---- upstream settings ----

func (s *Server) upstreamJSON() map[string]any {
	id, sec := s.upstreamCreds()
	var conns int
	s.db.QueryRow(`SELECT COUNT(*) FROM connections`).Scan(&conns)
	return map[string]any{
		"clientId": id, "hasSecret": sec != "", "configured": id != "",
		"callbackUrl": s.callbackURL(), "blockedSeconds": int(s.blockedFor().Seconds()),
		"connections": conns,
	}
}

func (s *Server) apiUpstream(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.upstreamJSON())
}

func (s *Server) apiUpstreamSave(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		ClearSecret  bool   `json:"clearSecret"`
	}
	if !decode(r, &in) {
		bad(w, "invalid json")
		return
	}
	in.ClientID = strings.TrimSpace(in.ClientID)
	if in.ClientID == "" {
		bad(w, "clientId is required")
		return
	}
	if old := s.getSetting("upstream_client_id"); old != "" && old != in.ClientID {
		// refresh tokens belong to the old Spotify client and can never work again
		s.db.Exec(`DELETE FROM connections`)
	}
	s.setSetting("upstream_client_id", in.ClientID)
	if in.ClearSecret {
		s.setSetting("upstream_client_secret", "")
	} else if sec := strings.TrimSpace(in.ClientSecret); sec != "" {
		s.setSetting("upstream_client_secret", s.seal(sec))
	}
	s.ccMu.Lock()
	s.ccToken = ""
	s.ccMu.Unlock()
	writeJSON(w, 200, s.upstreamJSON())
}

// ---- apps ----

func (s *Server) appJSON(a *App, withSecret bool) map[string]any {
	var conns, reqs, errs int
	s.db.QueryRow(`SELECT COUNT(*) FROM connections WHERE app_id=?`, a.ID).Scan(&conns)
	s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status>=400),0) FROM requests WHERE app_id=? AND ts>=?`, a.ID, time.Now().Unix()-86400).Scan(&reqs, &errs)
	m := map[string]any{
		"id": a.ID, "name": a.Name, "clientId": a.ClientID, "redirectUris": a.RedirectURIs,
		"allowedScopes": a.AllowedScopes, "rateLimit": a.RateLimit, "enabled": a.Enabled,
		"createdAt": a.CreatedAt, "connections": conns, "requests24h": reqs, "errors24h": errs,
	}
	if withSecret {
		sec, _ := s.open(a.SecretEnc)
		m["clientSecret"] = sec
	}
	return m
}

func (s *Server) apiApps(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT ` + appCols + ` FROM apps ORDER BY created_at`)
	if err != nil {
		bad(w, err.Error())
		return
	}
	var apps []*App
	for rows.Next() {
		if a, err := scanApp(rows); err == nil {
			apps = append(apps, a)
		}
	}
	rows.Close()
	out := []map[string]any{}
	for _, a := range apps {
		out = append(out, s.appJSON(a, false))
	}
	writeJSON(w, 200, out)
}

type appInput struct {
	Name          *string   `json:"name"`
	RedirectURIs  *[]string `json:"redirectUris"`
	AllowedScopes *string   `json:"allowedScopes"`
	RateLimit     *int      `json:"rateLimit"`
	Enabled       *bool     `json:"enabled"`
}

func validRedirect(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme == "" || p.Fragment != "" || strings.Contains(u, "#") {
		return false
	}
	switch strings.ToLower(p.Scheme) {
	case "javascript", "data", "vbscript", "file", "about", "blob":
		return false
	case "http": // like Spotify: plain http only for loopback
		h := p.Hostname()
		return h == "127.0.0.1" || h == "::1" || h == "localhost"
	case "https":
		return p.Host != ""
	}
	return true // custom app schemes, e.g. myapp://callback
}

// apply validates and copies input onto a; returns an error message or "".
func (in *appInput) apply(a *App) string {
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len(n) > 64 {
			return "name must be 1-64 characters"
		}
		a.Name = n
	}
	if in.RedirectURIs != nil {
		seen := map[string]bool{}
		uris := []string{}
		for _, u := range *in.RedirectURIs {
			if u = strings.TrimSpace(u); u == "" || seen[u] {
				continue
			}
			if !validRedirect(u) {
				return "invalid redirect URI: " + u + " (use https, a loopback http URL, or a custom scheme)"
			}
			seen[u] = true
			uris = append(uris, u)
		}
		a.RedirectURIs = uris
	}
	if in.AllowedScopes != nil {
		a.AllowedScopes = strings.Join(splitScopes(*in.AllowedScopes), " ")
	}
	if in.RateLimit != nil {
		if *in.RateLimit < 0 {
			return "rateLimit must be >= 0"
		}
		a.RateLimit = *in.RateLimit
	}
	if in.Enabled != nil {
		a.Enabled = *in.Enabled
	}
	return ""
}

func (s *Server) apiAppCreate(w http.ResponseWriter, r *http.Request) {
	var in appInput
	if !decode(r, &in) || in.Name == nil {
		bad(w, "name is required")
		return
	}
	a := &App{ID: randHex(8), ClientID: randHex(16), Enabled: true, CreatedAt: time.Now().Unix()}
	if msg := in.apply(a); msg != "" {
		bad(w, msg)
		return
	}
	a.SecretEnc = s.seal(randHex(16))
	uris, _ := json.Marshal(a.RedirectURIs)
	if _, err := s.db.Exec(`INSERT INTO apps(`+appCols+`) VALUES(?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.ClientID, a.SecretEnc, string(uris), a.AllowedScopes, a.RateLimit, 1, a.CreatedAt); err != nil {
		bad(w, err.Error())
		return
	}
	writeJSON(w, 201, s.appJSON(a, true))
}

func (s *Server) appFrom(w http.ResponseWriter, r *http.Request) *App {
	a := s.appByID(r.PathValue("id"))
	if a == nil {
		writeJSON(w, 404, map[string]string{"error": "app not found"})
	}
	return a
}

func (s *Server) apiAppGet(w http.ResponseWriter, r *http.Request) {
	if a := s.appFrom(w, r); a != nil {
		writeJSON(w, 200, s.appJSON(a, true))
	}
}

func (s *Server) apiAppUpdate(w http.ResponseWriter, r *http.Request) {
	a := s.appFrom(w, r)
	if a == nil {
		return
	}
	var in appInput
	if !decode(r, &in) {
		bad(w, "invalid json")
		return
	}
	if msg := in.apply(a); msg != "" {
		bad(w, msg)
		return
	}
	uris, _ := json.Marshal(a.RedirectURIs)
	en := 0
	if a.Enabled {
		en = 1
	}
	s.db.Exec(`UPDATE apps SET name=?, redirect_uris=?, allowed_scopes=?, rate_limit=?, enabled=? WHERE id=?`,
		a.Name, string(uris), a.AllowedScopes, a.RateLimit, en, a.ID)
	writeJSON(w, 200, s.appJSON(a, true))
}

func (s *Server) apiAppDelete(w http.ResponseWriter, r *http.Request) {
	if a := s.appFrom(w, r); a != nil {
		s.db.Exec(`DELETE FROM apps WHERE id=?`, a.ID) // cascades connections, tokens, codes
		w.WriteHeader(204)
	}
}

func (s *Server) apiAppRotate(w http.ResponseWriter, r *http.Request) {
	a := s.appFrom(w, r)
	if a == nil {
		return
	}
	a.SecretEnc = s.seal(randHex(16))
	s.db.Exec(`UPDATE apps SET secret_enc=? WHERE id=?`, a.SecretEnc, a.ID)
	writeJSON(w, 200, s.appJSON(a, true))
}

// ---- connected users ----

func (s *Server) apiConnections(w http.ResponseWriter, r *http.Request) {
	a := s.appFrom(w, r)
	if a == nil {
		return
	}
	rows, err := s.db.Query(`SELECT id,user_id,display_name,scope,created_at,last_used FROM connections WHERE app_id=? ORDER BY created_at DESC`, a.ID)
	if err != nil {
		bad(w, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, uid, name, scope string
		var created, last int64
		rows.Scan(&id, &uid, &name, &scope, &created, &last)
		out = append(out, map[string]any{"id": id, "userId": uid, "displayName": name, "scope": scope, "createdAt": created, "lastUsed": last})
	}
	writeJSON(w, 200, out)
}

func (s *Server) apiConnectionRevoke(w http.ResponseWriter, r *http.Request) {
	if a := s.appFrom(w, r); a != nil {
		s.db.Exec(`DELETE FROM connections WHERE id=? AND app_id=?`, r.PathValue("cid"), a.ID)
		w.WriteHeader(204)
	}
}

func (s *Server) apiConnectionsRevokeAll(w http.ResponseWriter, r *http.Request) {
	if a := s.appFrom(w, r); a != nil {
		s.db.Exec(`DELETE FROM connections WHERE app_id=?`, a.ID)
		w.WriteHeader(204)
	}
}

// ---- usage ----

func (s *Server) apiUsage(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 168 {
		hours = 24
	}
	app := r.URL.Query().Get("app")
	now := time.Now().Unix()
	first := (now/3600)*3600 - int64(hours-1)*3600

	counts := map[int64][2]int{}
	args := []any{first}
	q := `SELECT (ts/3600)*3600, COUNT(*), COALESCE(SUM(status>=400),0) FROM requests WHERE ts>=?`
	if app != "" {
		q += ` AND app_id=?`
		args = append(args, app)
	}
	rows, err := s.db.Query(q+` GROUP BY 1`, args...)
	if err != nil {
		bad(w, err.Error())
		return
	}
	for rows.Next() {
		var t int64
		var c, e int
		rows.Scan(&t, &c, &e)
		counts[t] = [2]int{c, e}
	}
	rows.Close()
	buckets := []map[string]any{}
	total, errs := 0, 0
	for t := first; t <= now; t += 3600 {
		c := counts[t]
		buckets = append(buckets, map[string]any{"t": t, "count": c[0], "errors": c[1]})
		total += c[0]
		errs += c[1]
	}

	perApp := []map[string]any{}
	rows, err = s.db.Query(`SELECT r.app_id, COALESCE(a.name,'(deleted or unknown)'), COUNT(*), COALESCE(SUM(r.status>=400),0), COALESCE(AVG(r.ms),0)
		FROM requests r LEFT JOIN apps a ON a.id=r.app_id WHERE r.ts>=? GROUP BY r.app_id ORDER BY 3 DESC`, first)
	if err == nil {
		for rows.Next() {
			var id, name string
			var c, e int
			var avg float64
			rows.Scan(&id, &name, &c, &e, &avg)
			perApp = append(perApp, map[string]any{"appId": id, "name": name, "count": c, "errors": e, "avgMs": int(avg)})
		}
		rows.Close()
	}
	writeJSON(w, 200, map[string]any{"hours": hours, "total": total, "errors": errs, "buckets": buckets, "perApp": perApp})
}

func (s *Server) apiRequests(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT r.ts, r.app_id, COALESCE(a.name,''), r.user_id, r.method, r.path, r.status, r.ms FROM requests r LEFT JOIN apps a ON a.id=r.app_id`
	args := []any{}
	if app := r.URL.Query().Get("app"); app != "" {
		q += ` WHERE r.app_id=?`
		args = append(args, app)
	}
	rows, err := s.db.Query(q+` ORDER BY r.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		bad(w, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ts int64
		var appID, name, user, method, path string
		var status, ms int
		rows.Scan(&ts, &appID, &name, &user, &method, &path, &status, &ms)
		out = append(out, map[string]any{"ts": ts, "appId": appID, "app": name, "userId": user, "method": method, "path": path, "status": status, "ms": ms})
	}
	writeJSON(w, 200, out)
}
