package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type window struct {
	start time.Time
	n     int
}

// allow is a fixed one-minute window per app (0 = unlimited).
func (s *Server) allow(app *App) (ok bool, retry int) {
	if app.RateLimit <= 0 {
		return true, 0
	}
	s.rlMu.Lock()
	defer s.rlMu.Unlock()
	w := s.rl[app.ID]
	now := time.Now()
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &window{start: now}
		s.rl[app.ID] = w
	}
	if w.n >= app.RateLimit {
		return false, int(time.Minute.Seconds()-now.Sub(w.start).Seconds()) + 1
	}
	w.n++
	return true, 0
}

var hopHeaders = map[string]bool{
	"connection": true, "keep-alive": true, "proxy-authenticate": true, "proxy-authorization": true,
	"te": true, "trailer": true, "transfer-encoding": true, "upgrade": true,
	"content-length": true, "content-encoding": true,
}

// handleAPI proxies /v1/* to api.spotify.com/v1/*, swapping our bearer token
// for the real upstream token of the connection behind it.
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	w = sw
	appID, userID := "", ""
	defer func() { s.logReq(appID, userID, r.Method, r.URL.Path, sw.status, started) }()

	if r.Method == http.MethodOptions {
		s.handleCORS(w, r)
		return
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		apiErr(w, 401, "No token provided")
		return
	}
	var tApp string
	var tConn *string // nil for client_credentials tokens
	var exp int64
	err := s.db.QueryRow(`SELECT app_id, connection_id, expires FROM tokens WHERE hash=? AND kind='access'`,
		hashToken(strings.TrimPrefix(auth, "Bearer "))).Scan(&tApp, &tConn, &exp)
	if err != nil {
		apiErr(w, 401, "Invalid access token")
		return
	}
	if exp > 0 && exp < time.Now().Unix() {
		apiErr(w, 401, "The access token expired")
		return
	}
	app := s.appByID(tApp)
	if app == nil || !app.Enabled {
		apiErr(w, 401, "Invalid access token")
		return
	}
	appID = app.ID
	if tConn != nil {
		s.db.QueryRow(`SELECT user_id FROM connections WHERE id=?`, *tConn).Scan(&userID)
	}
	if ok, retry := s.allow(app); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		apiErr(w, 429, "API rate limit exceeded")
		return
	}
	if d := s.blockedFor(); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(d.Seconds())+1))
		apiErr(w, 429, "API rate limit exceeded")
		return
	}

	var body []byte
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body, err = io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
		if err != nil {
			apiErr(w, 413, "Request body too large")
			return
		}
	}

	resp, err := s.callUpstream(r, body, tConn, false)
	if err == nil && resp.StatusCode == 401 {
		resp.Body.Close() // upstream token may have been revoked early: refresh once and retry
		resp, err = s.callUpstream(r, body, tConn, true)
	}
	if err != nil {
		switch {
		case errors.Is(err, errInvalidGrant):
			apiErr(w, 401, "The access token expired")
		default:
			var ue *upError
			if errors.As(err, &ue) && ue.Status == 429 {
				if ue.RetryAfter != "" {
					w.Header().Set("Retry-After", ue.RetryAfter)
				}
				apiErr(w, 429, "API rate limit exceeded")
			} else {
				apiErr(w, 502, "Bad gateway: upstream unavailable")
			}
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		s.noteRateLimit(resp.Header.Get("Retry-After"))
	}
	if tConn != nil {
		go s.db.Exec(`UPDATE connections SET last_used=? WHERE id=?`, time.Now().Unix(), *tConn)
	}
	s.relay(w, resp)
}

func (s *Server) callUpstream(r *http.Request, body []byte, connID *string, force bool) (*http.Response, error) {
	var access string
	var err error
	if connID != nil {
		access, err = s.upstreamAccess(*connID, force)
	} else {
		access, err = s.upstreamAppToken(force)
	}
	if err != nil {
		return nil, err
	}
	u := s.cfg.APIURL + r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(r.Context(), r.Method, u, rd)
	for _, h := range []string{"Content-Type", "Accept", "Accept-Language", "If-None-Match", "If-Modified-Since", "User-Agent"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.Header.Set("Authorization", "Bearer "+access)
	return s.http.Do(req)
}

// relay copies the upstream response. JSON bodies and Location headers get
// api.spotify.com rewritten to our public URL so pagination `next`/`href`
// links keep going through the proxy.
func (s *Server) relay(w http.ResponseWriter, resp *http.Response) {
	for k, vs := range resp.Header {
		lk := strings.ToLower(k)
		if hopHeaders[lk] || strings.HasPrefix(lk, "access-control-") {
			continue
		}
		for _, v := range vs {
			if lk == "location" {
				v = s.rewrite(v)
			}
			w.Header().Add(k, v)
		}
	}
	cors(w)
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			apiErr(w, 502, "Bad gateway")
			return
		}
		b = []byte(s.rewrite(string(b)))
		w.Header().Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(resp.StatusCode)
		w.Write(b)
		return
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) rewrite(v string) string {
	return strings.ReplaceAll(v, s.cfg.APIURL+"/", s.cfg.PublicURL+"/")
}
