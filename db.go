package main

import (
	"database/sql"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS apps(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  client_id TEXT NOT NULL UNIQUE,
  secret_enc TEXT NOT NULL,
  redirect_uris TEXT NOT NULL,            -- JSON array
  allowed_scopes TEXT NOT NULL DEFAULT '', -- space separated, '' = any
  rate_limit INTEGER NOT NULL DEFAULT 0,   -- requests/minute, 0 = unlimited
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL
);

-- one upstream Spotify grant per (virtual app, Spotify user)
CREATE TABLE IF NOT EXISTS connections(
  id TEXT PRIMARY KEY,
  app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  scope TEXT NOT NULL DEFAULT '',
  refresh_enc TEXT NOT NULL,
  access_enc TEXT NOT NULL DEFAULT '',
  access_exp INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0,
  UNIQUE(app_id, user_id)
);

-- /authorize requests waiting for Spotify to call /callback
CREATE TABLE IF NOT EXISTS pending(
  id TEXT PRIMARY KEY,
  app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL,
  state TEXT NOT NULL,
  scope TEXT NOT NULL,
  challenge TEXT NOT NULL,
  verifier_enc TEXT NOT NULL,
  expires INTEGER NOT NULL
);

-- our authorization codes handed to virtual-app clients
CREATE TABLE IF NOT EXISTS codes(
  hash TEXT PRIMARY KEY,
  app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL,
  challenge TEXT NOT NULL,
  expires INTEGER NOT NULL
);

-- our access/refresh tokens (hashed). connection_id NULL = client_credentials
CREATE TABLE IF NOT EXISTS tokens(
  hash TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  connection_id TEXT REFERENCES connections(id) ON DELETE CASCADE,
  scope TEXT NOT NULL DEFAULT '',
  expires INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS tokens_conn ON tokens(connection_id);

CREATE TABLE IF NOT EXISTS requests(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts INTEGER NOT NULL,
  app_id TEXT NOT NULL,
  user_id TEXT NOT NULL DEFAULT '',
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  status INTEGER NOT NULL,
  ms INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS requests_ts ON requests(ts);
CREATE INDEX IF NOT EXISTS requests_app_ts ON requests(app_id, ts);
`

func openDB(dir string) (*sql.DB, error) {
	dsn := "file:" + filepath.Join(dir, "spotify-proxy.db") +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // keeps SQLite simple; never run nested queries
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return db, nil
}
