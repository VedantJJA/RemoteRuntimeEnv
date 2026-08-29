package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

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
	TimeLimit   time.Duration
	MemoryMB    int64
	CompileTime time.Duration
}

type Executor struct {
	cli       *client.Client
	workDir   string
	pidsLimit int64
}

func NewExecutor(workDir string) (*Executor, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Executor{cli: cli, workDir: workDir, pidsLimit: 64}, nil
}

func createTarArchive(filename, content string) io.Reader {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:  filename,
		Mode:  0777,
		Uid:   1000,
		Gid:   1000,
		Uname: "sandbox",
		Gname: "sandbox",
		Size:  int64(len(content)),
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	return &buf
}

func (e *Executor) Run(ctx context.Context, lang Language, code, stdin string, lim Limits) (*Result, error) {
	tarStream := createTarArchive(lang.SourceFile, code)

	var cmd []string
	if len(lang.CompileCmd) > 0 {
		compileStr := strings.Join(lang.CompileCmd, " ")
		runStr := strings.Join(lang.RunCmd, " ")
		cmd = []string{"/bin/sh", "-c", fmt.Sprintf("%s && %s", compileStr, runStr)}
	} else {
		cmd = lang.RunCmd
	}

	totalTimeout := lim.TimeLimit
	if len(lang.CompileCmd) > 0 {
		totalTimeout += lim.CompileTime
	}

	runCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()

	start := time.Now()
	out, exitCode, err := e.runContainer(runCtx, lang.Image, cmd, tarStream, stdin, lim.MemoryMB, lim.MemoryMB)
	elapsed := time.Since(start)

	if runCtx.Err() == context.DeadlineExceeded {
		return &Result{Verdict: TimeLimitExceeded, WallTimeMS: elapsed.Milliseconds()}, nil
	}
	if err != nil {
		return &Result{Verdict: InternalError, Stderr: err.Error()}, nil
	}
	if exitCode == 137 {
		return &Result{Verdict: MemoryLimitExceeded, WallTimeMS: elapsed.Milliseconds()}, nil
	}
	verdict := Accepted
	if exitCode != 0 {
		if len(lang.CompileCmd) > 0 && !strings.Contains(out, "main") && (strings.Contains(out, "error") || strings.Contains(out, "Error")) {
			verdict = CompileError
		} else {
			verdict = RuntimeError
		}
	}
	return &Result{
		Verdict:    verdict,
		Stdout:     out,
		Stderr:     out,
		ExitCode:   exitCode,
		WallTimeMS: elapsed.Milliseconds(),
	}, nil
}

func (e *Executor) runContainer(ctx context.Context, image string, cmd []string, tarContent io.Reader, stdin string, memMB, swapMB int64) (string, int, error) {
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
			MemorySwap: swapMB * 1024 * 1024,
			CPUQuota:   100000,
			CPUPeriod:  100000,
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeTmpfs,
				Target: "/sandbox",
				TmpfsOptions: &mount.TmpfsOptions{
					Mode: 0777,
				},
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					Mode: 0777,
				},
			},
		},
	}

	resp, err := e.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", -1, fmt.Errorf("create: %w", err)
	}
	defer e.cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})

	if tarContent != nil {
		if err := e.cli.CopyToContainer(ctx, resp.ID, "/sandbox", tarContent, types.CopyToContainerOptions{AllowOverwriteDirWithFile: true}); err != nil {
			return "", -1, fmt.Errorf("copy source: %w", err)
		}
	}

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
		if len(stdin) > 0 {
			_, _ = io.Copy(attach.Conn, bytes.NewBufferString(stdin))
		}
		_ = attach.CloseWrite()
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
		_ = e.cli.ContainerKill(context.Background(), resp.ID, "SIGKILL")
		return "", -1, ctx.Err()
	case err := <-errCh:
		return "", -1, err
	case status := <-statusCh:
		<-copyDone
		combined := outBuf.String()
		if errBuf.Len() > 0 {
			if len(combined) > 0 {
				combined += "\n"
			}
			combined += errBuf.String()
		}
		return combined, int(status.StatusCode), nil
	}
}

func (e *Executor) Close() error { return e.cli.Close() }

func (e *Executor) PullImages(ctx context.Context) error {
	for _, l := range Languages {
		_, _, err := e.cli.ImageInspectWithRaw(ctx, l.Image)
		if err != nil {
			return fmt.Errorf("image %s not found locally: %w", l.Image, err)
		}
	}
	return nil
}