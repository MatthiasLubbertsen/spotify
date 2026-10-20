package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

// Config is read from the environment once at startup (see AGENTS.md 6a).
type Config struct {
	PublicURL    string // https://spotify.matthiaz.dev, no trailing slash
	Listen       string
	DataDir      string
	Key          [32]byte // AES-256 key derived from SECRET_KEY
	UserHeader   string   // header set by Traefik forward-auth (Tinyauth)
	AllowedUsers map[string]bool
	CallbackPath string // fixed upstream redirect path registered at Spotify

	// Optional seeds for the upstream app; the dashboard value wins once saved.
	SeedClientID     string
	SeedClientSecret string

	// Overridable so the e2e test can point at a fake Spotify.
	AccountsURL string
	APIURL      string
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func loadConfig() (*Config, error) {
	c := &Config{
		PublicURL:        strings.TrimRight(getenv("PUBLIC_URL", "https://spotify.matthiaz.dev"), "/"),
		Listen:           getenv("LISTEN", ":8080"),
		DataDir:          getenv("DATA_DIR", "./data"),
		UserHeader:       getenv("DASHBOARD_USER_HEADER", "Remote-User"),
		CallbackPath:     getenv("UPSTREAM_CALLBACK_PATH", "/callback"),
		SeedClientID:     getenv("SPOTIFY_CLIENT_ID", ""),
		SeedClientSecret: getenv("SPOTIFY_CLIENT_SECRET", ""),
		AccountsURL:      strings.TrimRight(getenv("SPOTIFY_ACCOUNTS_URL", "https://accounts.spotify.com"), "/"),
		APIURL:           strings.TrimRight(getenv("SPOTIFY_API_URL", "https://api.spotify.com"), "/"),
		AllowedUsers:     map[string]bool{},
	}
	for _, u := range strings.Split(os.Getenv("DASHBOARD_ALLOWED_USERS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			c.AllowedUsers[u] = true
		}
	}
	if !strings.HasPrefix(c.CallbackPath, "/") {
		return nil, fmt.Errorf("UPSTREAM_CALLBACK_PATH must start with /")
	}
	k := os.Getenv("SECRET_KEY")
	if len(k) < 32 {
		return nil, fmt.Errorf("SECRET_KEY must be set and at least 32 characters (openssl rand -base64 32)")
	}
	c.Key = sha256.Sum256([]byte(k))
	return c, nil
}
