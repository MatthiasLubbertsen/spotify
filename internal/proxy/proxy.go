// Package proxy forwards /v1/* Web API calls to Spotify using the upstream token behind each virtual-app token.
package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"spotis/internal/store"
	"spotis/internal/upstream"
	"spotis/internal/usage"
	"spotis/internal/util"
)

const maxRewrite = 64 << 20

type Handler struct {
	St        *store.Store
	Up        *upstream.Client
	PublicURL string

	mu      sync.Mutex
	windows map[string]*window
}

type window struct {
	start time.Time
	n     int
}

func New(st *store.Store, up *upstream.Client, publicURL string) *Handler {
	return &Handler{St: st, Up: up, PublicURL: publicURL, windows: map[string]*window{}}
}

var hopHeaders = map[string]bool{"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true,
	"Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true}

// allow applies the per-app fixed one-minute window rate limit.
func (h *Handler) allow(app *store.App) (ok bool, retry int) {
	if app.RateLimit <= 0 {
		return true, 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.windows[app.ID]
	if w == nil || time.Since(w.start) >= time.Minute {
		w = &window{start: time.Now()}
		h.windows[app.ID] = w
	}
	if w.n >= app.RateLimit {
		return false, int(time.Until(w.start.Add(time.Minute)).Seconds()) + 1
	}
	w.n++
	return true, 0
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	meta := usage.From(r.Context())
	authz := r.Header.Get("Authorization")
	if len(authz) < 8 || !strings.EqualFold(authz[:7], "bearer ") {
		util.SpotifyError(w, 401, "No token provided")
		return
	}
	tok, err := h.St.GetToken(util.Hash(strings.TrimSpace(authz[7:])), "access")
	if err != nil {
		util.SpotifyError(w, 401, "Invalid access token")
		return
	}
	if tok.ExpiresAt < time.Now().Unix() {
		util.SpotifyError(w, 401, "The access token expired")
		return
	}
	app, err := h.St.GetApp(tok.AppID)
	if err != nil || !app.Enabled {
		util.SpotifyError(w, 401, "Invalid access token")
		return
	}
	meta.AppID, meta.GrantID = app.ID, tok.GrantID
	if ok, retry := h.allow(app); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		util.SpotifyError(w, 429, "API rate limit exceeded")
		return
	}
	if left := h.Up.CooldownLeft(); left > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(left))
		util.SpotifyError(w, 429, "Spotify rate limit in effect for the shared upstream app")
		return
	}
	if tok.GrantID != 0 {
		h.St.TouchGrant(tok.GrantID)
	}

	// Buffer small bodies so a stale upstream token can be retried transparently.
	var body []byte
	if r.Body != nil && r.ContentLength != 0 {
		if r.ContentLength > 0 && r.ContentLength <= 1<<20 {
			body, _ = io.ReadAll(r.Body)
		}
	}
	retryable := r.Body == nil || r.ContentLength == 0 || body != nil

	for attempt := 0; attempt < 2; attempt++ {
		access, err := h.upstreamToken(r.Context(), tok)
		if err != nil {
			if errors.Is(err, upstream.ErrReauth) {
				util.SpotifyError(w, 401, "The access token expired")
			} else {
				util.SpotifyError(w, 502, "Upstream authorization failed")
			}
			return
		}
		resp, err := h.forward(r, body, access)
		if err != nil {
			util.SpotifyError(w, 502, "Bad gateway")
			return
		}
		if resp.StatusCode == 401 && attempt == 0 && retryable {
			resp.Body.Close()
			if tok.GrantID != 0 {
				h.Up.Invalidate(tok.GrantID)
			} else {
				h.Up.InvalidateCC()
			}
			continue
		}
		meta.UpstreamStatus = resp.StatusCode
		if resp.StatusCode == 429 {
			h.Up.SetCooldown(resp.Header.Get("Retry-After"))
		}
		h.write(w, resp)
		return
	}
}

func (h *Handler) upstreamToken(ctx context.Context, tok *store.Token) (string, error) {
	if tok.GrantID != 0 {
		return h.Up.AccessForGrant(ctx, tok.GrantID)
	}
	return h.Up.AccessClientCredentials(ctx)
}

func (h *Handler) forward(r *http.Request, body []byte, access string) (*http.Response, error) {
	u := h.Up.APIURL + r.URL.Path
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	var rd io.Reader
	switch {
	case body != nil:
		rd = bytes.NewReader(body)
	case r.Body != nil && r.ContentLength != 0:
		rd = r.Body
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u, rd)
	if err != nil {
		return nil, err
	}
	if rd != nil {
		req.ContentLength = r.ContentLength
	}
	for k, vs := range r.Header {
		switch http.CanonicalHeaderKey(k) {
		case "Authorization", "Accept-Encoding", "Cookie", "Host", "Content-Length", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-Real-Ip", "Forwarded":
			continue
		}
		if hopHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Authorization", "Bearer "+access)
	return h.Up.HTTP.Do(req)
}

// write relays the upstream response, rewriting Spotify API URLs in JSON so
// paging links (next/previous/href) keep pointing at this server.
func (h *Handler) write(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	hd := w.Header()
	for k, vs := range resp.Header {
		if hopHeaders[http.CanonicalHeaderKey(k)] || k == "Content-Length" || k == "Content-Encoding" {
			continue
		}
		for _, v := range vs {
			hd.Add(k, strings.ReplaceAll(v, h.Up.APIURL, h.PublicURL))
		}
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "json") && resp.ContentLength <= maxRewrite {
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxRewrite))
		if err != nil {
			util.SpotifyError(w, 502, "Bad gateway")
			return
		}
		// Raw and JSON-escaped (\/) forms.
		b = bytes.ReplaceAll(b, []byte(h.Up.APIURL), []byte(h.PublicURL))
		b = bytes.ReplaceAll(b, []byte(strings.ReplaceAll(h.Up.APIURL, "/", `\/`)), []byte(strings.ReplaceAll(h.PublicURL, "/", `\/`)))
		hd.Set("Content-Length", strconv.Itoa(len(b)))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(b)
		return
	}
	w.WriteHeader(resp.StatusCode)
	buf := make([]byte, 32<<10)
	fl, _ := w.(http.Flusher)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
