// Package store is the SQLite persistence layer.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Store struct{ db *sql.DB }

const schema = `
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS apps (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, secret_hash TEXT NOT NULL,
  redirect_uris TEXT NOT NULL DEFAULT '[]', allowed_scopes TEXT NOT NULL DEFAULT '',
  rate_limit INTEGER NOT NULL DEFAULT 0, allow_cc INTEGER NOT NULL DEFAULT 1,
  enabled INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL, secret_rotated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT, spotify_id TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, last_seen_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS grants (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scope TEXT NOT NULL DEFAULT '', refresh_enc TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'ok',
  created_at INTEGER NOT NULL, last_used_at INTEGER NOT NULL DEFAULT 0, UNIQUE(app_id, user_id));
CREATE TABLE IF NOT EXISTS tokens (
  hash TEXT PRIMARY KEY, kind TEXT NOT NULL,
  app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  grant_id INTEGER REFERENCES grants(id) ON DELETE CASCADE,
  scope TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS tokens_grant ON tokens(grant_id);
CREATE TABLE IF NOT EXISTS auth_requests (
  id TEXT PRIMARY KEY, app_id TEXT NOT NULL, redirect_uri TEXT NOT NULL, scope TEXT NOT NULL,
  state TEXT NOT NULL, code_challenge TEXT NOT NULL, expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS auth_codes (
  hash TEXT PRIMARY KEY, app_id TEXT NOT NULL, grant_id INTEGER NOT NULL REFERENCES grants(id) ON DELETE CASCADE,
  redirect_uri TEXT NOT NULL, code_challenge TEXT NOT NULL, expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, app_id TEXT NOT NULL DEFAULT '',
  grant_id INTEGER NOT NULL DEFAULT 0, kind TEXT NOT NULL, method TEXT NOT NULL, path TEXT NOT NULL,
  status INTEGER NOT NULL, upstream_status INTEGER NOT NULL DEFAULT 0, duration_ms INTEGER NOT NULL, bytes INTEGER NOT NULL DEFAULT 0);
CREATE INDEX IF NOT EXISTS requests_ts ON requests(ts);
CREATE INDEX IF NOT EXISTS requests_app_ts ON requests(app_id, ts);
`

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().Unix() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- settings ----

func (s *Store) GetSetting(k string) string {
	var v string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, k).Scan(&v)
	return v
}

func (s *Store) SetSetting(k, v string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v)
	return err
}

// ---- apps ----

type App struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	SecretHash      string   `json:"-"`
	RedirectURIs    []string `json:"redirect_uris"`
	AllowedScopes   []string `json:"allowed_scopes"`
	RateLimit       int      `json:"rate_limit_per_min"`
	AllowCC         bool     `json:"allow_client_credentials"`
	Enabled         bool     `json:"enabled"`
	CreatedAt       int64    `json:"created_at"`
	SecretRotatedAt int64    `json:"secret_rotated_at"`
	Users           int      `json:"users_count"`
	Requests24h     int      `json:"requests_24h"`
}

const appCols = `a.id,a.name,a.secret_hash,a.redirect_uris,a.allowed_scopes,a.rate_limit,a.allow_cc,a.enabled,a.created_at,a.secret_rotated_at,
 (SELECT COUNT(*) FROM grants g WHERE g.app_id=a.id),
 (SELECT COUNT(*) FROM requests r WHERE r.app_id=a.id AND r.ts>?)`

type scanner interface{ Scan(...any) error }

func scanApp(r scanner) (*App, error) {
	var a App
	var uris, scopes string
	var cc, en int
	if err := r.Scan(&a.ID, &a.Name, &a.SecretHash, &uris, &scopes, &a.RateLimit, &cc, &en, &a.CreatedAt, &a.SecretRotatedAt, &a.Users, &a.Requests24h); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(uris), &a.RedirectURIs)
	if a.RedirectURIs == nil {
		a.RedirectURIs = []string{}
	}
	a.AllowedScopes = strings.Fields(scopes)
	if a.AllowedScopes == nil {
		a.AllowedScopes = []string{}
	}
	a.AllowCC, a.Enabled = cc == 1, en == 1
	return &a, nil
}

func since24h() int64 { return time.Now().Add(-24 * time.Hour).UnixMilli() }

func (s *Store) CreateApp(a *App) error {
	uris, _ := json.Marshal(a.RedirectURIs)
	t := now()
	a.CreatedAt, a.SecretRotatedAt = t, t
	_, err := s.db.Exec(`INSERT INTO apps(id,name,secret_hash,redirect_uris,allowed_scopes,rate_limit,allow_cc,enabled,created_at,secret_rotated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.Name, a.SecretHash, string(uris), strings.Join(a.AllowedScopes, " "), a.RateLimit, b2i(a.AllowCC), b2i(a.Enabled), t, t)
	return err
}

func (s *Store) GetApp(id string) (*App, error) {
	a, err := scanApp(s.db.QueryRow(`SELECT `+appCols+` FROM apps a WHERE a.id=?`, since24h(), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) ListApps() ([]*App, error) {
	rows, err := s.db.Query(`SELECT `+appCols+` FROM apps a ORDER BY a.created_at`, since24h())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*App{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) UpdateApp(a *App) error {
	uris, _ := json.Marshal(a.RedirectURIs)
	_, err := s.db.Exec(`UPDATE apps SET name=?,redirect_uris=?,allowed_scopes=?,rate_limit=?,allow_cc=?,enabled=? WHERE id=?`,
		a.Name, string(uris), strings.Join(a.AllowedScopes, " "), a.RateLimit, b2i(a.AllowCC), b2i(a.Enabled), a.ID)
	return err
}

// SetAppSecret rotates the secret and revokes nothing else; existing tokens stay valid.
func (s *Store) SetAppSecret(id, hash string) error {
	_, err := s.db.Exec(`UPDATE apps SET secret_hash=?, secret_rotated_at=? WHERE id=?`, hash, now(), id)
	return err
}

func (s *Store) DeleteApp(id string) error {
	_, err := s.db.Exec(`DELETE FROM apps WHERE id=?`, id)
	return err
}

// ---- users & grants ----

type Grant struct {
	ID          int64  `json:"id"`
	AppID       string `json:"app_id"`
	AppName     string `json:"app_name"`
	UserID      int64  `json:"user_id"`
	SpotifyID   string `json:"spotify_id"`
	DisplayName string `json:"display_name"`
	Scope       string `json:"scope"`
	Status      string `json:"status"` // ok | reauth
	CreatedAt   int64  `json:"created_at"`
	LastUsedAt  int64  `json:"last_used_at"`
	RefreshEnc  string `json:"-"`
}

const grantCols = `g.id,g.app_id,a.name,g.user_id,u.spotify_id,u.display_name,g.scope,g.status,g.created_at,g.last_used_at,g.refresh_enc`
const grantFrom = ` FROM grants g JOIN apps a ON a.id=g.app_id JOIN users u ON u.id=g.user_id `

func scanGrant(r scanner) (*Grant, error) {
	var g Grant
	err := r.Scan(&g.ID, &g.AppID, &g.AppName, &g.UserID, &g.SpotifyID, &g.DisplayName, &g.Scope, &g.Status, &g.CreatedAt, &g.LastUsedAt, &g.RefreshEnc)
	return &g, err
}

// UpsertGrant records (or refreshes) the link between a virtual app and a Spotify user.
func (s *Store) UpsertGrant(appID, spotifyID, displayName, scope, refreshEnc string) (*Grant, error) {
	t := now()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var uid int64
	err = tx.QueryRow(`INSERT INTO users(spotify_id,display_name,created_at,last_seen_at) VALUES(?,?,?,?)
		ON CONFLICT(spotify_id) DO UPDATE SET display_name=excluded.display_name, last_seen_at=excluded.last_seen_at RETURNING id`,
		spotifyID, displayName, t, t).Scan(&uid)
	if err != nil {
		return nil, err
	}
	var gid int64
	err = tx.QueryRow(`INSERT INTO grants(app_id,user_id,scope,refresh_enc,status,created_at) VALUES(?,?,?,?, 'ok', ?)
		ON CONFLICT(app_id,user_id) DO UPDATE SET scope=excluded.scope, refresh_enc=excluded.refresh_enc, status='ok' RETURNING id`,
		appID, uid, scope, refreshEnc, t).Scan(&gid)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetGrant(gid)
}

func (s *Store) GetGrant(id int64) (*Grant, error) {
	g, err := scanGrant(s.db.QueryRow(`SELECT `+grantCols+grantFrom+`WHERE g.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

func (s *Store) ListGrants(appID string) ([]*Grant, error) {
	q := `SELECT ` + grantCols + grantFrom
	var args []any
	if appID != "" {
		q += `WHERE g.app_id=? `
		args = append(args, appID)
	}
	rows, err := s.db.Query(q+`ORDER BY g.created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) UpdateGrantRefresh(id int64, enc string) error {
	_, err := s.db.Exec(`UPDATE grants SET refresh_enc=? WHERE id=?`, enc, id)
	return err
}

func (s *Store) SetGrantStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE grants SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) TouchGrant(id int64) {
	_, _ = s.db.Exec(`UPDATE grants SET last_used_at=? WHERE id=? AND last_used_at<?`, now(), id, now()-30)
}

// DeleteGrant revokes one user's access to one app (tokens cascade).
func (s *Store) DeleteGrant(id int64) (bool, error) {
	r, err := s.db.Exec(`DELETE FROM grants WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// DeleteUser revokes a Spotify user across every app.
func (s *Store) DeleteUser(id int64) (bool, error) {
	r, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

// ---- tokens ----

type Token struct {
	Hash      string
	Kind      string // access | refresh
	AppID     string
	GrantID   int64 // 0 for client-credentials tokens
	Scope     string
	ExpiresAt int64 // unix seconds; 0 = never
}

func (s *Store) InsertToken(t Token) error {
	var gid any
	if t.GrantID != 0 {
		gid = t.GrantID
	}
	_, err := s.db.Exec(`INSERT INTO tokens(hash,kind,app_id,grant_id,scope,expires_at,created_at) VALUES(?,?,?,?,?,?,?)`,
		t.Hash, t.Kind, t.AppID, gid, t.Scope, t.ExpiresAt, now())
	return err
}

func (s *Store) GetToken(hash, kind string) (*Token, error) {
	var t Token
	var gid sql.NullInt64
	err := s.db.QueryRow(`SELECT hash,kind,app_id,grant_id,scope,expires_at FROM tokens WHERE hash=? AND kind=?`, hash, kind).
		Scan(&t.Hash, &t.Kind, &t.AppID, &gid, &t.Scope, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	t.GrantID = gid.Int64
	return &t, err
}

// ---- auth requests / codes ----

type AuthRequest struct {
	ID, AppID, RedirectURI, Scope, State, CodeChallenge string
	ExpiresAt                                           int64
}

func (s *Store) InsertAuthRequest(r AuthRequest) error {
	_, err := s.db.Exec(`INSERT INTO auth_requests VALUES(?,?,?,?,?,?,?)`, r.ID, r.AppID, r.RedirectURI, r.Scope, r.State, r.CodeChallenge, r.ExpiresAt)
	return err
}

// TakeAuthRequest returns and deletes the request (single use).
func (s *Store) TakeAuthRequest(id string) (*AuthRequest, error) {
	var r AuthRequest
	err := s.db.QueryRow(`DELETE FROM auth_requests WHERE id=? RETURNING id,app_id,redirect_uri,scope,state,code_challenge,expires_at`, id).
		Scan(&r.ID, &r.AppID, &r.RedirectURI, &r.Scope, &r.State, &r.CodeChallenge, &r.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && r.ExpiresAt < now()) {
		return nil, ErrNotFound
	}
	return &r, err
}

type AuthCode struct {
	Hash, AppID, RedirectURI, CodeChallenge string
	GrantID, ExpiresAt                      int64
}

func (s *Store) InsertAuthCode(c AuthCode) error {
	_, err := s.db.Exec(`INSERT INTO auth_codes VALUES(?,?,?,?,?,?)`, c.Hash, c.AppID, c.GrantID, c.RedirectURI, c.CodeChallenge, c.ExpiresAt)
	return err
}

func (s *Store) TakeAuthCode(hash string) (*AuthCode, error) {
	var c AuthCode
	err := s.db.QueryRow(`DELETE FROM auth_codes WHERE hash=? RETURNING hash,app_id,grant_id,redirect_uri,code_challenge,expires_at`, hash).
		Scan(&c.Hash, &c.AppID, &c.GrantID, &c.RedirectURI, &c.CodeChallenge, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && c.ExpiresAt < now()) {
		return nil, ErrNotFound
	}
	return &c, err
}

// Cleanup removes expired short-lived rows and old request logs.
func (s *Store) Cleanup(retentionDays int) {
	t := now()
	_, _ = s.db.Exec(`DELETE FROM auth_requests WHERE expires_at<?`, t)
	_, _ = s.db.Exec(`DELETE FROM auth_codes WHERE expires_at<?`, t)
	_, _ = s.db.Exec(`DELETE FROM tokens WHERE kind='access' AND expires_at<?`, t-3600)
	_, _ = s.db.Exec(`DELETE FROM requests WHERE ts<?`, time.Now().AddDate(0, 0, -retentionDays).UnixMilli())
}
