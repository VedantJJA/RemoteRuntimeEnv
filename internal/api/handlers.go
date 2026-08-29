package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"rre/internal/db"
	"rre/internal/queue"
	"rre/internal/runner"
)

type Server struct {
	exec *runner.Executor
	pool *queue.Pool
	db   *db.DB
	mux  *http.ServeMux
}

func NewServer(exec *runner.Executor, pool *queue.Pool, database *db.DB) *Server {
	s := &Server{exec: exec, pool: pool, db: database, mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/submit", s.handleSubmit)
	s.mux.HandleFunc("/api/result", s.handleResult)
	s.mux.HandleFunc("/api/leaderboard", s.handleLeaderboard)
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

type submitReq struct {
	ProblemID string `json:"problem_id"`
	Language  string `json:"language"`
	Code      string `json:"code"`
	Stdin     string `json:"stdin"`
	// Per-problem limits, set by whoever authored the problem — a tight
	// limit on an easy problem and a generous one on a hard problem is how
	// "efficiency matched" gets enforced per-task rather than globally.
	TimeLimitMS int64 `json:"time_limit_ms"`
	MemoryMB    int64 `json:"memory_mb"`
}

type submitResp struct {
	ID string `json:"id"`
}

// handleSubmit validates the request, queues the (potentially slow)
// container execution on the worker pool, and returns immediately with an
// ID. This keeps the HTTP handler itself fast and lets the pool apply
// backpressure independently of how many HTTP connections are open.
func (s *Server) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req submitReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	lang, ok := runner.Languages[req.Language]
	if !ok {
		http.Error(w, "unsupported language", http.StatusBadRequest)
		return
	}
	if req.TimeLimitMS <= 0 || req.TimeLimitMS > 10000 {
		req.TimeLimitMS = 2000 // sane default + hard ceiling regardless of client input
	}
	if req.MemoryMB <= 0 || req.MemoryMB > 512 {
		req.MemoryMB = 128
	}

	id := uuid.NewString()
	// Pre-register a PENDING row synchronously so /api/result never 404s
	// for a submission that was genuinely accepted, even before the worker
	// picks it up.
	_ = s.db.Insert(db.Submission{
		ID: id, ProblemID: req.ProblemID, Language: req.Language,
		Verdict: "PENDING", CreatedAt: time.Now(),
	})

	s.pool.Submit(queue.Job{Run: func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := s.exec.Run(ctx, lang, req.Code, req.Stdin, runner.Limits{
			TimeLimit:   time.Duration(req.TimeLimitMS) * time.Millisecond,
			MemoryMB:    req.MemoryMB,
			CompileTime: 10 * time.Second,
		})
		if err != nil {
			res = &runner.Result{Verdict: runner.InternalError, Stderr: err.Error()}
		}
		s.db.Insert(db.Submission{
			ID: id, ProblemID: req.ProblemID, Language: req.Language,
			Verdict: string(res.Verdict), WallTimeMS: res.WallTimeMS,
			PeakMemKB: res.PeakMemKB, CreatedAt: time.Now(),
		})
	}})

	writeJSON(w, submitResp{ID: id})
}

func (s *Server) handleResult(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	sub, err := s.db.Get(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, sub)
}

func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	problem := r.URL.Query().Get("problem_id")
	rows, err := s.db.Leaderboard(problem, 50)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
