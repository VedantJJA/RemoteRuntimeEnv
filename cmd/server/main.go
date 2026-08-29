package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"runtime"

	"rre/internal/api"
	"rre/internal/db"
	"rre/internal/queue"
	"rre/internal/runner"
)

func main() {
	workDir := getenv("RRE_WORKDIR", "/var/lib/rre/submissions")
	dbPath := getenv("RRE_DB", "/var/lib/rre/rre.db")
	defaultAddr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		defaultAddr = ":" + port
	}
	addr := getenv("RRE_ADDR", defaultAddr)

	exec, err := runner.NewExecutor(workDir)
	if err != nil {
		log.Fatalf("executor: %v", err)
	}
	defer exec.Close()

	if err := exec.PullImages(context.Background()); err != nil {
		log.Fatalf("images not ready: %v (build them from /images first)", err)
	}

	database, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()

	// One worker per CPU core is the deliberate ceiling: each running
	// container gets a full CPU quota (see docker.go), so oversubscribing
	// workers beyond core count would just make every submission slower
	// instead of actually running more concurrently. Queue depth of 100
	// gives headroom before submissions start getting rejected/blocked.
	workers := runtime.NumCPU()
	pool := queue.NewPool(workers, 100)
	defer pool.Shutdown()

	srv := api.NewServer(exec, pool, database)
	log.Printf("listening on %s with %d workers", addr, workers)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
