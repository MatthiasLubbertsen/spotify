// Package usage records request logs for the dashboard.
package usage

import (
	"context"
	"net/http"
	"time"

	"spotis/internal/store"
)

type Meta struct {
	AppID          string
	GrantID        int64
	UpstreamStatus int
}

type ctxKey struct{}

// From returns the per-request metadata holder, or a throwaway one.
func From(ctx context.Context) *Meta {
	if m, ok := ctx.Value(ctxKey{}).(*Meta); ok {
		return m
	}
	return &Meta{}
}

type Logger struct {
	ch chan store.Request
}

func NewLogger(st *store.Store) *Logger {
	l := &Logger{ch: make(chan store.Request, 2048)}
	go l.run(st)
	return l
}

func (l *Logger) run(st *store.Store) {
	var batch []store.Request
	t := time.NewTicker(time.Second)
	flush := func() {
		if len(batch) > 0 {
			_ = st.InsertRequests(batch)
			batch = batch[:0]
		}
	}
	for {
		select {
		case r := <-l.ch:
			batch = append(batch, r)
			if len(batch) >= 200 {
				flush()
			}
		case <-t.C:
			flush()
		}
	}
}

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *recorder) WriteHeader(s int) {
	if r.status == 0 {
		r.status = s
	}
	r.ResponseWriter.WriteHeader(s)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Wrap logs every request through next. kind is "oauth" or "api". Query strings are
// only kept for api requests (oauth queries carry codes and state).
func (l *Logger) Wrap(kind string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		m := &Meta{}
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), ctxKey{}, m)))
		if rec.status == 0 {
			rec.status = 200
		}
		path := r.URL.Path
		if kind == "api" && r.URL.RawQuery != "" {
			q := r.URL.RawQuery
			if len(q) > 200 {
				q = q[:200]
			}
			path += "?" + q
		}
		select {
		case l.ch <- store.Request{TS: start.UnixMilli(), AppID: m.AppID, GrantID: m.GrantID, Kind: kind, Method: r.Method,
			Path: path, Status: rec.status, UpstreamStatus: m.UpstreamStatus, DurationMS: time.Since(start).Milliseconds(), Bytes: rec.bytes}:
		default: // never block traffic on logging
		}
	})
}
