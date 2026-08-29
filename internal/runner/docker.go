package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Verdict mirrors the classic online-judge status set. Keeping this small
// and explicit (rather than free-text errors) is what lets the leaderboard
// and frontend treat "why did this fail" consistently.
type Verdict string

const (
	Accepted            Verdict = "ACCEPTED"
	CompileError        Verdict = "COMPILE_ERROR"
	RuntimeError        Verdict = "RUNTIME_ERROR"
	TimeLimitExceeded   Verdict = "TIME_LIMIT_EXCEEDED"
	MemoryLimitExceeded Verdict = "MEMORY_LIMIT_EXCEEDED"
	InternalError       Verdict = "INTERNAL_ERROR"
)

type Result struct {
	Verdict     Verdict
	Stdout      string
	Stderr      string
	ExitCode    int
	WallTimeMS  int64
	PeakMemKB   int64
	CompileLog  string
}

type Limits struct {
	TimeLimit   time.Duration // wall clock for the RUN phase only
	MemoryMB    int64
	CompileTime time.Duration // fixed generous cap, not part of scoring
}

type Executor struct {
	cli       *client.Client
	workDir   string // host path where submission source trees are staged
	pidsLimit int64
}

func NewExecutor(workDir string) (*Executor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return nil, err
	}
	return &Executor{cli: cli, workDir: workDir, pidsLimit: 64}, nil
}

// Run stages the submission on disk, compiles it if needed, then executes it
// inside a locked-down container and reports the verdict + measured cost.
// A fresh container is used per submission (never reused) so state from one
// participant's run can never leak into another's — this is what makes
// results reproducible/deterministic across retries.
func (e *Executor) Run(ctx context.Context, lang Language, code, stdin string, lim Limits) (*Result, error) {
	subDir := filepath.Join(e.workDir, fmt.Sprintf("sub-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(subDir, 0755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(subDir)

	if err := os.WriteFile(filepath.Join(subDir, lang.SourceFile), []byte(code), 0644); err != nil {
		return nil, err
	}

	if len(lang.CompileCmd) > 0 {
		compileCtx, cancel := context.WithTimeout(ctx, lim.CompileTime)
		defer cancel()
		out, exitCode, err := e.runContainer(compileCtx, lang.Image, lang.CompileCmd, subDir, "", 512, 0)
		if err != nil {
			return &Result{Verdict: InternalError, CompileLog: err.Error()}, nil
		}
		if exitCode != 0 {
			return &Result{Verdict: CompileError, CompileLog: out, ExitCode: exitCode}, nil
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, lim.TimeLimit)
	defer cancel()

	start := time.Now()
	out, exitCode, err := e.runContainer(runCtx, lang.Image, lang.RunCmd, subDir, stdin, lim.MemoryMB, lim.MemoryMB)
	elapsed := time.Since(start)

	if runCtx.Err() == context.DeadlineExceeded {
		return &Result{Verdict: TimeLimitExceeded, WallTimeMS: elapsed.Milliseconds()}, nil
	}
	if err != nil {
		return &Result{Verdict: InternalError, Stderr: err.Error()}, nil
	}
	// Docker reports OOM-killed containers with exit code 137 (128+SIGKILL).
	// We treat that specifically as MLE rather than a generic runtime crash.
	if exitCode == 137 {
		return &Result{Verdict: MemoryLimitExceeded, WallTimeMS: elapsed.Milliseconds()}, nil
	}
	verdict := Accepted
	if exitCode != 0 {
		verdict = RuntimeError
	}
	return &Result{
		Verdict:    verdict,
		Stdout:     out,
		ExitCode:   exitCode,
		WallTimeMS: elapsed.Milliseconds(),
	}, nil
}

// runContainer creates a single-use, network-isolated, read-only-rootfs
// container, feeds it stdin, and returns combined stdout+stderr and its
// exit code. memMB/swapMB set equal disables swap so an over-limit process
// is OOM-killed immediately instead of thrashing the host.
func (e *Executor) runContainer(ctx context.Context, image string, cmd []string, srcDir, stdin string, memMB, swapMB int64) (string, int, error) {
	cfg := &container.Config{
		Image:        image,
		Cmd:          cmd,
		WorkingDir:   "/sandbox",
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		OpenStdin:    true,
		StdinOnce:    true,
	}
	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		ReadonlyRootfs: true,
		Resources: container.Resources{
			PidsLimit:  &e.pidsLimit,
			Memory:     memMB * 1024 * 1024,
			MemorySwap: swapMB * 1024 * 1024, // == Memory -> swap disabled
			CPUQuota:   100000,               // 1 CPU
			CPUPeriod:  100000,
		},
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: srcDir, Target: "/sandbox"},
			{Type: mount.TypeTmpfs, Target: "/tmp"}, // writable scratch only
		},
	}

	resp, err := e.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", -1, fmt.Errorf("create: %w", err)
	}
	defer e.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})

	attach, err := e.cli.ContainerAttach(ctx, resp.ID, container.AttachOptions{
		Stream: true, Stdin: true, Stdout: true, Stderr: true,
	})
	if err != nil {
		return "", -1, fmt.Errorf("attach: %w", err)
	}
	defer attach.Close()

	if err := e.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", -1, fmt.Errorf("start: %w", err)
	}

	go func() {
		io.Copy(attach.Conn, bytes.NewBufferString(stdin))
		attach.CloseWrite()
	}()

	var outBuf, errBuf bytes.Buffer
	copyDone := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(&outBuf, &errBuf, attach.Reader)
		copyDone <- err
	}()

	statusCh, errCh := e.cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case <-ctx.Done():
		// Hard kill: this is what turns an infinite loop into a clean
		// TLE verdict instead of a hung request.
		e.cli.ContainerKill(context.Background(), resp.ID, "SIGKILL")
		return "", -1, ctx.Err()
	case err := <-errCh:
		return "", -1, err
	case status := <-statusCh:
		<-copyDone
		combined := outBuf.String()
		if errBuf.Len() > 0 {
			combined += "\n[stderr]\n" + errBuf.String()
		}
		return combined, int(status.StatusCode), nil
	}
}

func (e *Executor) Close() error { return e.cli.Close() }

// PullImages makes sure every language image exists locally before the
// server starts accepting traffic, so the first submission of a contest
// doesn't pay a cold pull.
func (e *Executor) PullImages(ctx context.Context) error {
	for _, l := range Languages {
		_, _, err := e.cli.ImageInspectWithRaw(ctx, l.Image)
		if err != nil {
			return fmt.Errorf("image %s not found locally, build it first (see /images): %w", l.Image, err)
		}
	}
	return nil
}
