package history

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kshaikh/scada/internal/core/tag"
)

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tag_id TEXT NOT NULL,
		ts INTEGER NOT NULL,
		value TEXT NOT NULL,
		quality TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_history_tag_ts ON history(tag_id, ts)`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Record(tagID string, snap tag.Snapshot) error {
	_, err := s.db.Exec(`INSERT INTO history(tag_id, ts, value, quality) VALUES (?,?,?,?)`,
		tagID, snap.Timestamp.UnixMilli(), fmt.Sprintf("%v", snap.Value), snap.Quality)
	return err
}

func (s *Store) Query(tagID string, from, to time.Time, limit int) ([]tag.Snapshot, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT ts, value, quality FROM history WHERE tag_id=? AND ts>=? AND ts<=? ORDER BY ts ASC LIMIT ?`,
		tagID, from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tag.Snapshot
	for rows.Next() {
		var ts int64
		var val, q string
		if err := rows.Scan(&ts, &val, &q); err != nil {
			return nil, err
		}
		out = append(out, tag.Snapshot{
			ID:        tagID,
			Value:     val,
			Quality:   q,
			Timestamp: time.UnixMilli(ts),
		})
	}
	return out, rows.Err()
}
