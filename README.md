# Remote Runtime Environment (RRE)

A backend for running untrusted, participant-submitted code in a
deterministic, resource-bounded sandbox and ranking submissions by
efficiency  built for a speed-coding event where participants compete on
algorithmic efficiency, not just correctness.

## How it works

```
HTTP request → validate + queue → worker pool (N = CPU cores)
                                        │
                                        ▼
                         fresh, single-use Docker container
                         (no network, read-only rootfs, pids
                          capped, memory+swap capped, 1 CPU)
                                        │
                                        ▼
                        compile (if needed) → run with stdin
                                        │
                                        ▼
                    verdict + wall time + exit code → SQLite
```

1. `POST /api/submit` writes the submitted code to a scratch directory on
   the host, queues a job, and returns immediately with a submission ID.
2. A worker picks the job up and asks Docker for a **brand-new container**
   scoped to that one submission  nothing is reused between runs, which is
   what makes results reproducible (no leftover state, files, or processes
   from a previous submission can affect the next one).
3. The container has no network access, a read-only root filesystem (only
   `/sandbox` and a `tmpfs` at `/tmp` are writable), a process-count cap, and
   a memory+swap cap. It runs as a non-root user.
4. The run is wrapped in a Go `context.WithTimeout`. If it doesn't finish in
   time, the container is `SIGKILL`ed and the submission is marked
   `TIME_LIMIT_EXCEEDED`  this is what turns an infinite loop into a clean,
   bounded failure instead of a hung request or a stuck worker.
5. `GET /api/result?id=...` polls for the verdict; `GET
   /api/leaderboard?problem_id=...` ranks all `ACCEPTED` submissions for a
   problem by wall time, then peak memory.

## Why these choices

- **Docker containers, not raw processes.** `rlimit`/`cgroup`-only sandboxing
  is lighter but leaves the filesystem and network exposed unless you build
  that isolation yourself. Docker gives network isolation, a disposable
  filesystem, and resource limits as first-class flags, which is a much
  smaller surface to get wrong under a deadline.
- **A worker pool sized to CPU count**, not one goroutine per request. Each
  container gets a full CPU quota, so running more containers than cores
  doesn't add throughput  it just makes everything slower and lets a burst
  of submissions take the whole VM down. Capping concurrency and letting
  extra submissions queue is the entire scalability/cost story for a
  single-VM deployment: the host degrades gracefully (slower responses)
  instead of catastrophically (OOM/crash).
- **SQLite**, not Postgres. A single VM with no expectation of concurrent
  writers beyond the worker pool doesn't need a client-server database 
  one file, zero ops, easy to inspect and reset.
- **Per-language pre-built images**, pulled/verified at startup. Avoids
  paying image-pull latency on a participant's first submission of a
  language, and keeps each image minimal (no compilers in the Python image,
  no Node in the C++ image).

## Handling disruptions (per the task brief)

| Disruption | Mitigation |
|---|---|
| Infinite loop | `context.WithTimeout` around the run phase + hard `ContainerKill` on deadline → `TIME_LIMIT_EXCEEDED` |
| Memory leak / overflow | `--memory` and `--memory-swap` set equal (swap disabled) → Docker OOM-kills the container (exit 137), reported as `MEMORY_LIMIT_EXCEEDED` |
| Fork bomb / process preemption | `--pids-limit` caps live processes inside the container regardless of what the code spawns |
| Runaway CPU use starving other jobs | `CPUQuota`/`CPUPeriod` cap each container to one CPU; the worker pool caps how many run at once |
| Submission crashing the host process | Execution happens in a child container, not in-process; a crash there can't take down the Go server |

## Determinism & reproducibility

- Every run gets a fresh container  no shared filesystem or process state
  between submissions, even from the same participant.
- No network access, so a submission can't fetch external state (a random
  seed from the internet, a different answer on retry, etc.) that would make
  results non-reproducible.
- The same source + same stdin + same limits always run through the exact
  same image and resource profile.

## Access control & security

- Containers run as a non-root user inside a read-only rootfs  even a
  container-escape bug has no write access to anything but `/sandbox` and a
  throwaway `tmpfs`.
- No network namespace access at all (`--network none`) removes the most
  common abuse path (data exfiltration, DoS against third parties, calling
  out to fetch a different payload).
- The Go server itself is the only thing with access to the Docker socket;
  it never executes participant code outside a container.
- Server-side limit ceilings (`10s` max time limit, `512MB` max memory)
  are enforced regardless of what the client requests, so a malicious
  client can't ask for unbounded resources.

## Known limitations / things to harden further

- No auth on the API yet  for the contest, this would sit behind whatever
  auth the frontend/judging platform provides (JWT check middleware would
  slot into `api/handlers.go`).
- Peak memory is currently only inferred from the OOM/exit-code signal, not
  measured continuously  a follow-up would poll `ContainerStats` during
  the run to record actual peak RSS for accepted submissions too.
- Single-VM deployment has a scaling ceiling  if this needed to grow past
  one machine, the worker pool would move to a real queue (Redis/NATS) so
  multiple VMs could pull from the same backlog.

## Running it

```bash
# 1. Build the sandbox images (one-time, or after editing a Dockerfile)
./images/build.sh

# 2. Fetch Go deps and build the server
go mod tidy
go build -o bin/server ./cmd/server

# 3. Run (needs access to the Docker socket)
sudo mkdir -p /var/lib/rre/submissions
./bin/server
```

Example submission:

```bash
curl -X POST localhost:8080/api/submit -d '{
  "problem_id": "two-sum",
  "language": "python",
  "code": "print(sum(map(int, input().split())))",
  "stdin": "2 3",
  "time_limit_ms": 2000,
  "memory_mb": 128
}'
# => {"id": "..."}

curl "localhost:8080/api/result?id=<id>"
curl "localhost:8080/api/leaderboard?problem_id=two-sum"
```

## Deploying on the Oracle Cloud VM

1. Install Docker on the VM, add a dedicated `rre` system user to the
   `docker` group (don't run this as root).
2. `scp` the repo over, run `images/build.sh` and `go build` on the VM (or
   cross-compile locally with `GOOS=linux GOARCH=arm64 go build`, matching
   Oracle's Ampere ARM shape).
3. Copy `deploy/rre.service` to `/etc/systemd/system/`, `systemctl enable
   --now rre`.
4. Put the VM behind a reverse proxy (Caddy/nginx) for TLS if it's
   internet-facing.
