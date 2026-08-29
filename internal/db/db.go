package db

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Submission struct {
	ID         string    `json:"id"`
	ProblemID  string    `json:"problem_id"`
	Language   string    `json:"language"`
	Verdict    string    `json:"verdict"`
	Stdout     string    `json:"stdout,omitempty"`
	Stderr     string    `json:"stderr,omitempty"`
	WallTimeMS int64     `json:"wall_time_ms"`
	PeakMemKB  int64     `json:"peak_mem_kb"`
	CreatedAt  time.Time `json:"created_at"`
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
		stdout TEXT DEFAULT '',
		stderr TEXT DEFAULT '',
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
	_, _ = conn.Exec("ALTER TABLE submissions ADD COLUMN stdout TEXT DEFAULT ''")
	_, _ = conn.Exec("ALTER TABLE submissions ADD COLUMN stderr TEXT DEFAULT ''")
	return &DB{conn: conn}, nil
}

func (d *DB) Insert(s Submission) error {
	_, err := d.conn.Exec(
		`INSERT INTO submissions (id, problem_id, language, verdict, stdout, stderr, wall_time_ms, peak_mem_kb, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET verdict=excluded.verdict, stdout=excluded.stdout, stderr=excluded.stderr,
		 	wall_time_ms=excluded.wall_time_ms, peak_mem_kb=excluded.peak_mem_kb, created_at=excluded.created_at`,
		s.ID, s.ProblemID, s.Language, s.Verdict, s.Stdout, s.Stderr, s.WallTimeMS, s.PeakMemKB, s.CreatedAt,
	)
	return err
}

func (d *DB) Get(id string) (*Submission, error) {
	row := d.conn.QueryRow(`SELECT id, problem_id, language, verdict, stdout, stderr, wall_time_ms, peak_mem_kb, created_at
		FROM submissions WHERE id = ?`, id)
	var s Submission
	if err := row.Scan(&s.ID, &s.ProblemID, &s.Language, &s.Verdict, &s.Stdout, &s.Stderr, &s.WallTimeMS, &s.PeakMemKB, &s.CreatedAt); err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *DB) Leaderboard(problemID string, limit int) ([]Submission, error) {
	rows, err := d.conn.Query(
		`SELECT id, problem_id, language, verdict, stdout, stderr, wall_time_ms, peak_mem_kb, created_at
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
		if err := rows.Scan(&s.ID, &s.ProblemID, &s.Language, &s.Verdict, &s.Stdout, &s.Stderr, &s.WallTimeMS, &s.PeakMemKB, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (d *DB) Close() error { return d.conn.Close() }