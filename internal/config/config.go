// Package config loads runtime settings from the environment.
package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr          string   // listen address
	PublicURL     string   // externally visible base URL, no trailing slash
	DataDir       string   // holds the SQLite file and generated key
	SecretKey     string   // optional; derives the at-rest encryption key
	AccountsURL   string   // upstream accounts host (overridable for tests)
	APIURL        string   // upstream Web API host (overridable for tests)
	AllowedUsers  []string // optional dashboard allowlist (Tinyauth user or email)
	RetentionDays int      // how long request logs are kept
}

func Load() Config {
	c := Config{
		Addr:          env("SPOTIS_ADDR", ":8080"),
		PublicURL:     strings.TrimRight(env("SPOTIS_PUBLIC_URL", "https://spotify.matthiaz.dev"), "/"),
		DataDir:       env("SPOTIS_DATA_DIR", "./data"),
		SecretKey:     os.Getenv("SPOTIS_SECRET_KEY"),
		AccountsURL:   strings.TrimRight(env("SPOTIS_UPSTREAM_ACCOUNTS_URL", "https://accounts.spotify.com"), "/"),
		APIURL:        strings.TrimRight(env("SPOTIS_UPSTREAM_API_URL", "https://api.spotify.com"), "/"),
		RetentionDays: 30,
	}
	if n, err := strconv.Atoi(os.Getenv("SPOTIS_RETENTION_DAYS")); err == nil && n > 0 {
		c.RetentionDays = n
	}
	for _, u := range strings.Split(os.Getenv("SPOTIS_DASHBOARD_USERS"), ",") {
		if u = strings.TrimSpace(u); u != "" {
			c.AllowedUsers = append(c.AllowedUsers, strings.ToLower(u))
		}
	}
	return c
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
