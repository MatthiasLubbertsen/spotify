package store

import (
	"fmt"
	"time"
)

type Request struct {
	ID             int64  `json:"id"`
	TS             int64  `json:"ts"` // unix ms
	AppID          string `json:"app_id"`
	AppName        string `json:"app_name"`
	GrantID        int64  `json:"grant_id"`
	Kind           string `json:"kind"` // oauth | api
	Method         string `json:"method"`
	Path           string `json:"path"`
	Status         int    `json:"status"`
	UpstreamStatus int    `json:"upstream_status"`
	DurationMS     int64  `json:"duration_ms"`
	Bytes          int64  `json:"bytes"`
}

func (s *Store) InsertRequests(rs []Request) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	st, err := tx.Prepare(`INSERT INTO requests(ts,app_id,grant_id,kind,method,path,status,upstream_status,duration_ms,bytes) VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, r := range rs {
		if _, err := st.Exec(r.TS, r.AppID, r.GrantID, r.Kind, r.Method, r.Path, r.Status, r.UpstreamStatus, r.DurationMS, r.Bytes); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListRequests returns recent requests, newest first. errorsOnly keeps status>=400.
func (s *Store) ListRequests(appID string, limit int, errorsOnly bool) ([]*Request, error) {
	q := `SELECT r.id,r.ts,r.app_id,COALESCE(a.name,''),r.grant_id,r.kind,r.method,r.path,r.status,r.upstream_status,r.duration_ms,r.bytes
	 FROM requests r LEFT JOIN apps a ON a.id=r.app_id WHERE 1=1`
	var args []any
	if appID != "" {
		q += ` AND r.app_id=?`
		args = append(args, appID)
	}
	if errorsOnly {
		q += ` AND r.status>=400`
	}
	q += ` ORDER BY r.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Request{}
	for rows.Next() {
		var r Request
		if err := rows.Scan(&r.ID, &r.TS, &r.AppID, &r.AppName, &r.GrantID, &r.Kind, &r.Method, &r.Path, &r.Status, &r.UpstreamStatus, &r.DurationMS, &r.Bytes); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

type Point struct {
	T      int64 `json:"t"` // bucket start, unix ms
	Total  int   `json:"total"`
	Errors int   `json:"errors"`
}
type PathStat struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}
type AppStat struct {
	AppID   string `json:"app_id"`
	AppName string `json:"app_name"`
	Total   int    `json:"total"`
	Errors  int    `json:"errors"`
}
type Usage struct {
	Series   []Point    `json:"series"`
	Total    int        `json:"total"`
	Errors   int        `json:"errors"`
	Limited  int        `json:"rate_limited"`
	AvgMS    float64    `json:"avg_ms"`
	TopPaths []PathStat `json:"top_paths"`
	PerApp   []AppStat  `json:"per_app"`
}

// GetUsage aggregates request logs for the window, bucketed by bucket.
func (s *Store) GetUsage(appID string, window, bucket time.Duration) (*Usage, error) {
	from := time.Now().Add(-window).UnixMilli()
	bms := bucket.Milliseconds()
	where, args := `r.ts>=?`, []any{from}
	if appID != "" {
		where += ` AND r.app_id=?`
		args = append(args, appID)
	}
	u := &Usage{Series: []Point{}, TopPaths: []PathStat{}, PerApp: []AppStat{}}
	byBucket := map[int64]*Point{}

	rows, err := s.db.Query(fmt.Sprintf(`SELECT (r.ts/%d)*%d, COUNT(*), SUM(r.status>=400) FROM requests r WHERE %s GROUP BY 1`, bms, bms, where), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p.T, &p.Total, &p.Errors); err != nil {
			rows.Close()
			return nil, err
		}
		byBucket[p.T] = &p
		u.Total += p.Total
		u.Errors += p.Errors
	}
	rows.Close()
	end := (time.Now().UnixMilli() / bms) * bms
	for t := end - (int64(window/bucket)-1)*bms; t <= end; t += bms {
		if p, ok := byBucket[t]; ok {
			u.Series = append(u.Series, *p)
		} else {
			u.Series = append(u.Series, Point{T: t})
		}
	}

	_ = s.db.QueryRow(`SELECT COUNT(*), COALESCE(AVG(duration_ms),0) FROM requests r WHERE `+where+` AND r.status=429`, args...).Scan(&u.Limited, new(float64))
	_ = s.db.QueryRow(`SELECT COALESCE(AVG(duration_ms),0) FROM requests r WHERE `+where, args...).Scan(&u.AvgMS)

	rows, err = s.db.Query(`SELECT r.path, COUNT(*) c FROM requests r WHERE `+where+` AND r.kind='api' GROUP BY r.path ORDER BY c DESC LIMIT 8`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p PathStat
		if err := rows.Scan(&p.Path, &p.Count); err != nil {
			rows.Close()
			return nil, err
		}
		u.TopPaths = append(u.TopPaths, p)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT r.app_id, COALESCE(a.name,'(deleted)'), COUNT(*), SUM(r.status>=400) FROM requests r LEFT JOIN apps a ON a.id=r.app_id
	 WHERE `+where+` AND r.app_id<>'' GROUP BY r.app_id ORDER BY 3 DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AppStat
		if err := rows.Scan(&a.AppID, &a.AppName, &a.Total, &a.Errors); err != nil {
			return nil, err
		}
		u.PerApp = append(u.PerApp, a)
	}
	return u, rows.Err()
}
