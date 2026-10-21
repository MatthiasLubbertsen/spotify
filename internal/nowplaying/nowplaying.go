// Package nowplaying serves GET /now-playing: a tiny pollable endpoint guarded by an API key
// (no OAuth for the caller). It reads the connected user's playback through their grant.
package nowplaying

import (
	"context"
	"encoding/json"
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

const cacheTTL = 3 * time.Second

type Handler struct {
	St *store.Store
	Up *upstream.Client

	mu    sync.Mutex
	cache map[int64]cached
}

type cached struct {
	body []byte
	exp  time.Time
}

func New(st *store.Store, up *upstream.Client) *Handler {
	return &Handler{St: st, Up: up, cache: map[int64]cached{}}
}

type item struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	Image      string   `json:"image"`
	DurationMS int      `json:"duration_ms"`
	URL        string   `json:"url"`
}

type out struct {
	IsPlaying  bool   `json:"is_playing"`
	ProgressMS int    `json:"progress_ms"`
	Type       string `json:"type,omitempty"`
	Item       *item  `json:"item"`
}

func keyFrom(r *http.Request) string {
	if k := r.Header.Get("X-API-Key"); k != "" {
		return k
	}
	if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "bearer ") {
		return strings.TrimSpace(a[7:])
	}
	return r.URL.Query().Get("key")
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-API-Key")
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	meta := usage.From(r.Context())
	key := keyFrom(r)
	if key == "" {
		util.SpotifyError(w, 401, "API key required (X-API-Key header, Bearer token or ?key=)")
		return
	}
	k, err := h.St.FindAPIKey(util.Hash(key))
	if err != nil {
		util.SpotifyError(w, 401, "Invalid API key")
		return
	}
	meta.AppID, meta.GrantID = k.AppID, k.GrantID
	app, err := h.St.GetApp(k.AppID)
	g, gerr := h.St.GetGrant(k.GrantID)
	if err != nil || gerr != nil || !app.Enabled {
		util.SpotifyError(w, 401, "Invalid API key")
		return
	}
	if !strings.Contains(" "+g.Scope+" ", " user-read-currently-playing ") && !strings.Contains(" "+g.Scope+" ", " user-read-playback-state ") {
		util.SpotifyError(w, 403, "The connected user did not grant user-read-currently-playing; log in again with that scope")
		return
	}
	h.St.TouchAPIKey(k.ID)
	h.St.TouchGrant(g.ID)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	h.mu.Lock()
	c, ok := h.cache[g.ID]
	h.mu.Unlock()
	if ok && time.Now().Before(c.exp) {
		_, _ = w.Write(c.body)
		return
	}
	if left := h.Up.CooldownLeft(); left > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(left))
		util.SpotifyError(w, 429, "Spotify rate limit in effect for the shared upstream app")
		return
	}
	body, status, err := h.fetch(r.Context(), g.ID)
	meta.UpstreamStatus = status
	switch {
	case errors.Is(err, upstream.ErrReauth):
		util.SpotifyError(w, 401, "The connected user must log in again")
		return
	case err != nil:
		util.SpotifyError(w, 502, "Could not reach Spotify")
		return
	case status == 429:
		w.Header().Set("Retry-After", "5")
		util.SpotifyError(w, 429, "Spotify rate limit")
		return
	case status != 200:
		util.SpotifyError(w, 502, "Spotify returned "+strconv.Itoa(status))
		return
	}
	h.mu.Lock()
	h.cache[g.ID] = cached{body, time.Now().Add(cacheTTL)}
	h.mu.Unlock()
	_, _ = w.Write(body)
}

// fetch asks Spotify what the user is playing and returns the simplified JSON.
func (h *Handler) fetch(ctx context.Context, grantID int64) ([]byte, int, error) {
	access, err := h.Up.AccessForGrant(ctx, grantID)
	if err != nil {
		return nil, 0, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, h.Up.APIURL+"/v1/me/player/currently-playing?additional_types=episode", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := h.Up.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		h.Up.SetCooldown(resp.Header.Get("Retry-After"))
		return nil, 429, nil
	}
	if resp.StatusCode == http.StatusNoContent {
		b, _ := json.Marshal(out{})
		return b, 200, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var in struct {
		IsPlaying  bool   `json:"is_playing"`
		ProgressMS int    `json:"progress_ms"`
		Type       string `json:"currently_playing_type"`
		Item       *struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			DurationMS int    `json:"duration_ms"`
			Ext        struct {
				Spotify string `json:"spotify"`
			} `json:"external_urls"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album *struct {
				Name   string `json:"name"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"album"`
			Show *struct {
				Name   string `json:"name"`
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"show"`
			Images []struct {
				URL string `json:"url"`
			} `json:"images"`
		} `json:"item"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if len(raw) == 0 {
		b, _ := json.Marshal(out{})
		return b, 200, nil
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, 0, err
	}
	o := out{IsPlaying: in.IsPlaying, ProgressMS: in.ProgressMS, Type: in.Type}
	if in.Item != nil {
		it := &item{ID: in.Item.ID, Name: in.Item.Name, DurationMS: in.Item.DurationMS, URL: in.Item.Ext.Spotify, Artists: []string{}}
		for _, a := range in.Item.Artists {
			it.Artists = append(it.Artists, a.Name)
		}
		switch {
		case in.Item.Album != nil:
			it.Album = in.Item.Album.Name
			if len(in.Item.Album.Images) > 0 {
				it.Image = in.Item.Album.Images[0].URL
			}
		case in.Item.Show != nil:
			it.Album = in.Item.Show.Name
			if len(in.Item.Show.Images) > 0 {
				it.Image = in.Item.Show.Images[0].URL
			}
		}
		if it.Image == "" && len(in.Item.Images) > 0 {
			it.Image = in.Item.Images[0].URL
		}
		o.Item = it
	}
	b, _ := json.Marshal(o)
	return b, 200, nil
}
