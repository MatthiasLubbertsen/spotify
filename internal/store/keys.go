package store

import (
	"database/sql"
	"errors"
)

// APIKey lets a caller poll the now-playing endpoint for one grant (app x Spotify user).
type APIKey struct {
	ID          int64  `json:"id"`
	GrantID     int64  `json:"grant_id"`
	AppID       string `json:"app_id"`
	AppName     string `json:"app_name"`
	SpotifyID   string `json:"spotify_id"`
	DisplayName string `json:"display_name"`
	Label       string `json:"label"`
	Prefix      string `json:"prefix"`
	CreatedAt   int64  `json:"created_at"`
	LastUsedAt  int64  `json:"last_used_at"`
}

const keyCols = `k.id,k.grant_id,g.app_id,a.name,u.spotify_id,u.display_name,k.label,k.prefix,k.created_at,k.last_used_at`
const keyFrom = ` FROM api_keys k JOIN grants g ON g.id=k.grant_id JOIN apps a ON a.id=g.app_id JOIN users u ON u.id=g.user_id `

func scanKey(r scanner) (*APIKey, error) {
	var k APIKey
	err := r.Scan(&k.ID, &k.GrantID, &k.AppID, &k.AppName, &k.SpotifyID, &k.DisplayName, &k.Label, &k.Prefix, &k.CreatedAt, &k.LastUsedAt)
	return &k, err
}

func (s *Store) CreateAPIKey(grantID int64, label, hash, prefix string) (*APIKey, error) {
	var id int64
	err := s.db.QueryRow(`INSERT INTO api_keys(hash,grant_id,label,prefix,created_at) VALUES(?,?,?,?,?) RETURNING id`,
		hash, grantID, label, prefix, now()).Scan(&id)
	if err != nil {
		return nil, err
	}
	k, err := scanKey(s.db.QueryRow(`SELECT `+keyCols+keyFrom+`WHERE k.id=?`, id))
	return k, err
}

func (s *Store) ListAPIKeys() ([]*APIKey, error) {
	rows, err := s.db.Query(`SELECT ` + keyCols + keyFrom + `ORDER BY k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*APIKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// FindAPIKey resolves a key hash, or ErrNotFound.
func (s *Store) FindAPIKey(hash string) (*APIKey, error) {
	k, err := scanKey(s.db.QueryRow(`SELECT `+keyCols+keyFrom+`WHERE k.hash=?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return k, err
}

func (s *Store) TouchAPIKey(id int64) {
	_, _ = s.db.Exec(`UPDATE api_keys SET last_used_at=? WHERE id=? AND last_used_at<?`, now(), id, now()-10)
}

func (s *Store) DeleteAPIKey(id int64) (bool, error) {
	r, err := s.db.Exec(`DELETE FROM api_keys WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}
