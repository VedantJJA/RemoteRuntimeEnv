package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// Using modernc.org/sqlite (pure Go, no cgo) rather than mattn/go-sqlite3
// keeps the build a static binary — no C toolchain needed on the deploy VM,
// which matters when you're cross-compiling or want a minimal Docker image
// for the backend itself.

type Submission struct {
	ID         string
	ProblemID  string
	Language   string
	Verdict    string
	WallTimeMS int64
	PeakMemKB  int64
	CreatedAt  time.Time
}

type DB struct{ conn *sql.DB }

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	schema := `
	CREATE TABLE IF NOT EXISTS submissions (
		id TEXT PRIMARY KEY,
		problem_id TEXT NOT NULL,
		language TEXT NOT NULL,
		verdict TEXT NOT NULL,
		wall_time_ms INTEGER NOT NULL,
		peak_mem_kb INTEGER NOT NULL,
		created_at DATETIME NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_problem_leaderboard
		ON submissions(problem_id, verdict, wall_time_ms);
	`
	if _, err := conn.Exec(schema); err != nil {
		return nil, err
	}
	return &DB{conn: conn}, nil
}

// Insert is an upsert keyed on id: the handler writes a PENDING row when a
// submission is queued, then overwrites it with the final verdict once the
// worker finishes, so a client polling /api/result never sees a 404 for a
// submission that was genuinely accepted.
func (d *DB) Insert(s Submission) error {
	_, err := d.conn.Exec(
		`INSERT INTO submissions (id, problem_id, language, verdict, wall_time_ms, peak_mem_kb, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET verdict=excluded.verdict, wall_time_ms=excluded.wall_time_ms,
		 	peak_mem_kb=excluded.peak_mem_kb, created_at=excluded.created_at`,
		s.ID, s.ProblemID, s.Language, s.Verdict, s.WallTimeMS, s.PeakMemKB, s.CreatedAt,
	)
	return err
}

func (d *DB) Get(id string) (*Submission, error) {
	row := d.conn.QueryRow(`SELECT id, problem_id, language, verdict, wall_time_ms, peak_mem_kb, created_at
		FROM submissions WHERE id = ?`, id)
	var s Submission
	if err := row.Scan(&s.ID, &s.ProblemID, &s.Language, &s.Verdict, &s.WallTimeMS, &s.PeakMemKB, &s.CreatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

// Leaderboard ranks only ACCEPTED submissions by wall time then memory —
// the same two axes the task brief asks us to match efficiency on.
func (d *DB) Leaderboard(problemID string, limit int) ([]Submission, error) {
	rows, err := d.conn.Query(
		`SELECT id, problem_id, language, verdict, wall_time_ms, peak_mem_kb, created_at
		 FROM submissions
		 WHERE problem_id = ? AND verdict = 'ACCEPTED'
		 ORDER BY wall_time_ms ASC, peak_mem_kb ASC
		 LIMIT ?`, problemID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Submission
	for rows.Next() {
		var s Submission
		if err := rows.Scan(&s.ID, &s.ProblemID, &s.Language, &s.Verdict, &s.WallTimeMS, &s.PeakMemKB, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (d *DB) Close() error { return d.conn.Close() }
