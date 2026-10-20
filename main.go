package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

//go:embed all:web/dist
var webFS embed.FS

type Server struct {
	cfg  *Config
	db   *sql.DB
	http *http.Client // upstream Spotify calls

	connLocks sync.Map // connection id -> *sync.Mutex (serialises upstream refresh)

	ccMu      sync.Mutex // cached upstream client_credentials token
	ccToken   string
	ccExpires time.Time

	rlMu   sync.Mutex
	rl     map[string]*window
	blkMu  sync.Mutex
	blkTil time.Time // global upstream 429 back-off

	logCh chan reqLog
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" { // for the container HEALTHCHECK (distroless has no curl)
		port := strings.TrimPrefix(getenv("LISTEN", ":8080"), ":")
		resp, err := http.Get("http://127.0.0.1:" + port + "/healthz")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	db, err := openDB(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	s := newServer(cfg, db)
	log.Printf("listening on %s, public url %s", cfg.Listen, cfg.PublicURL)
	log.Fatal((&http.Server{Addr: cfg.Listen, Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}

func newServer(cfg *Config, db *sql.DB) *Server {
	s := &Server{
		cfg:   cfg,
		db:    db,
		http:  &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		rl:    map[string]*window{},
		logCh: make(chan reqLog, 2048),
	}
	s.seedSettings()
	go s.logWriter()
	go s.janitor()
	return s
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Public: Spotify-compatible surface (own client auth + tokens protect it).
	mux.HandleFunc("GET /authorize", s.handleAuthorize)
	mux.HandleFunc("GET "+s.cfg.CallbackPath, s.handleCallback) // the one URI registered at Spotify
	mux.HandleFunc("POST /api/token", s.handleToken)
	mux.HandleFunc("OPTIONS /api/token", s.handleCORS)
	mux.HandleFunc("/v1/", s.handleAPI)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	// Public: landing page + static bundle.
	mux.HandleFunc("GET /{$}", s.serveIndex)
	mux.HandleFunc("GET /assets/", s.serveStatic)
	mux.HandleFunc("GET /favicon.svg", s.serveStatic)

	// Protected by Traefik forward-auth (Tinyauth): everything under /dashboard.
	mux.HandleFunc("GET /dashboard", s.dash(s.serveIndex))
	mux.HandleFunc("GET /dashboard/{rest...}", s.dash(s.serveIndex))
	s.dashboardAPI(mux)

	return mux
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/dashboard/api/") { // unknown API route, not an SPA page
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}
	b, err := fs.ReadFile(webFS, "web/dist/index.html")
	if err != nil {
		http.Error(w, "frontend not built", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	sub, _ := fs.Sub(webFS, "web/dist")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}

// ---- settings (key/value, secrets sealed) ----

func (s *Server) getSetting(k string) string {
	var v string
	s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, k).Scan(&v)
	return v
}

func (s *Server) setSetting(k, v string) {
	s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
}

func (s *Server) seedSettings() {
	if s.getSetting("upstream_client_id") == "" && s.cfg.SeedClientID != "" {
		s.setSetting("upstream_client_id", s.cfg.SeedClientID)
		if s.cfg.SeedClientSecret != "" {
			s.setSetting("upstream_client_secret", s.seal(s.cfg.SeedClientSecret))
		}
	}
}

func (s *Server) upstreamCreds() (id, secret string) {
	id = s.getSetting("upstream_client_id")
	secret, _ = s.open(s.getSetting("upstream_client_secret"))
	return
}

func (s *Server) callbackURL() string { return s.cfg.PublicURL + s.cfg.CallbackPath }

// ---- request log ----

type reqLog struct {
	ts                int64
	app, user, method string
	path              string
	status, ms        int
}

func (s *Server) logReq(app, user, method, path string, status int, started time.Time) {
	select {
	case s.logCh <- reqLog{time.Now().Unix(), app, user, method, path, status, int(time.Since(started).Milliseconds())}:
	default: // drop under extreme load rather than slow the proxy
	}
}

func (s *Server) logWriter() {
	for l := range s.logCh {
		s.db.Exec(`INSERT INTO requests(ts,app_id,user_id,method,path,status,ms) VALUES(?,?,?,?,?,?,?)`,
			l.ts, l.app, l.user, l.method, l.path, l.status, l.ms)
	}
}

// janitor prunes expired short-lived rows and old request logs.
func (s *Server) janitor() {
	for {
		now := time.Now().Unix()
		s.db.Exec(`DELETE FROM pending WHERE expires<?`, now)
		s.db.Exec(`DELETE FROM codes WHERE expires<?`, now)
		s.db.Exec(`DELETE FROM tokens WHERE expires>0 AND expires<?`, now)
		s.db.Exec(`DELETE FROM requests WHERE ts<?`, now-7*86400)
		time.Sleep(time.Hour)
	}
}

// ---- response helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// apiErr is the Web API error shape: {"error":{"status":..,"message":..}}.
func apiErr(w http.ResponseWriter, status int, msg string) {
	cors(w)
	writeJSON(w, status, map[string]any{"error": map[string]any{"status": status, "message": msg}})
}

// oauthErr is the RFC 6749 shape used by the token endpoint.
func oauthErr(w http.ResponseWriter, status int, code, desc string) {
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

func cors(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept, If-None-Match")
	h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	h.Set("Access-Control-Expose-Headers", "Retry-After, ETag")
}

func (s *Server) handleCORS(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.WriteHeader(http.StatusNoContent)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(c int) { w.status = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func splitScopes(s string) []string { return strings.Fields(s) }

// close drains the request-log queue (used by tests).
func (s *Server) close() {
	for len(s.logCh) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
}
