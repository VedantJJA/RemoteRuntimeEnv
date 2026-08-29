# Remote Runtime Environment (RRE)

[![Live Endpoint](https://img.shields.io/badge/Live%20PaaS-remoteruntimeenv.klouds.online-007acc?style=flat-square&logo=cloud)](https://remoteruntimeenv.klouds.online)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![Docker](https://img.shields.io/badge/Docker-Sandboxed-2496ED?style=flat-square&logo=docker)](https://www.docker.com/)
[![SQLite](https://img.shields.io/badge/SQLite-Pure%20Go-003B57?style=flat-square&logo=sqlite)](https://www.sqlite.org/)

A lightweight, deterministic code execution engine and online judge backend built for speed-coding competitions. RRE executes untrusted participant code inside isolated, disposable Docker containers with strict resource and security limits, ranking submissions by algorithmic efficiency (wall time and memory).

> 📖 **Full System Architecture & Evaluation Documentation**: See [documentation.html](./documentation.html) for detailed diagrams, security matrices, database indexing strategies, and rubric criteria breakdowns.

---

## ✨ Features

- **🛡️ Multi-Layer Sandboxing:** Disposable single-use containers, read-only root filesystems, unshared network namespaces (`--network none`), and in-memory `tmpfs` execution.
- **⚡ Deterministic Benchmarking:** Fresh environments per submission with zero state leakage and zero network variance.
- **⏱️ Hard Limits Enforcement:** Strict cgroup limits on CPU, memory, execution timeouts (`SIGKILL`), and process count (`pids-limit` = 64).
- **🚀 High Throughput & Low Latency:** CPU-bounded worker pool (`runtime.NumCPU()`) with graceful queue backpressure.
- **💾 Zero-Ops Database:** Pure Go SQLite engine with composite indexing and WAL mode for high-concurrency leaderboard queries.
- **🌐 Supported Languages:** Python 3.12, C++17, Go 1.22, and Node.js 20 (easily extensible).

---

## 🏗️ Architecture Overview

```
HTTP Request (POST /api/submit)
         │
         ▼
  Validate & Ingest ---> SQLite (Status: PENDING)
         │
         ▼
 Worker Pool (N = CPU Cores)
         │
         ▼
 Fresh Docker Container (No Network, Read-Only Rootfs, Cgroups, tmpfs)
         │
         ▼
 Compile (if needed) & Execute with Stdin
         │
         ▼
 Capture Verdict, Wall Time, Stdout, Stderr ---> SQLite (Status: ACCEPTED / Error)
```

---

## 🚀 Quickstart

### 1. Prerequisites
- [Go 1.22+](https://golang.org/dl/)
- [Docker Engine](https://docs.docker.com/get-docker/)

### 2. Build Sandbox Container Images
```bash
./images/build.sh
```

### 3. Run Locally
```bash
# Build the binary
go build -o bin/server ./cmd/server

# Start the server
./bin/server
```
The server will start listening on `http://localhost:8080`.

---

## 📡 API Reference

### 1. Submit Code: `POST /api/submit`
Enqueues a submission for evaluation.

```bash
curl -X POST http://localhost:8080/api/submit \
  -H "Content-Type: application/json" \
  -d '{
    "problem_id": "two-sum",
    "language": "python",
    "code": "print(sum(map(int, input().split())))",
    "stdin": "10 25",
    "time_limit_ms": 2000,
    "memory_mb": 128
  }'
```

**Response:**
```json
{
  "id": "7f1514f8-c45b-482c-b382-fa91cea5ef32"
}
```

### 2. Poll Result: `GET /api/result?id={id}`
Fetches the verdict and performance metrics.

```bash
curl "http://localhost:8080/api/result?id=7f1514f8-c45b-482c-b382-fa91cea5ef32"
```

**Response:**
```json
{
  "id": "7f1514f8-c45b-482c-b382-fa91cea5ef32",
  "problem_id": "two-sum",
  "language": "python",
  "verdict": "ACCEPTED",
  "stdout": "35\n",
  "stderr": "",
  "wall_time_ms": 38,
  "peak_mem_kb": 12480,
  "created_at": "2026-08-29T12:38:12.358Z"
}
```

### 3. View Leaderboard: `GET /api/leaderboard?problem_id={problem_id}`
Returns all `ACCEPTED` submissions sorted by wall time then memory.

```bash
curl "http://localhost:8080/api/leaderboard?problem_id=two-sum"
```

### 4. Health Check: `GET /healthz`
```bash
curl "http://localhost:8080/healthz"
```

---

## ⚙️ Configuration

RRE is configured via environment variables:

| Variable | Default | Description |
| :--- | :--- | :--- |
| `RRE_ADDR` | `:8080` | Server listen address |
| `PORT` | `8080` | Port fallback for cloud PaaS environments |
| `RRE_WORKDIR` | `/var/lib/rre/submissions` | Scratch directory for submission files |
| `RRE_DB` | `/var/lib/rre/rre.db` | Path to SQLite database file |

---

## ☁️ Deployment

- **Cloud PaaS (DinD):** Uses [klouds.yaml](./klouds.yaml) and [Dockerfile](./Dockerfile) with privileged container execution.
- **Linux VM (systemd):** Use the systemd unit file at [deploy/rre.service](./deploy/rre.service).

---

## 📄 Documentation

For full architectural deep-dives, sequence diagrams, security threat models, and extensibility guides, open [documentation.html](./documentation.html) in your browser.