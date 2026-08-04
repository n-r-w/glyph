// Package bash executes project commands in isolated process groups.
package bash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// Service runs bash commands.
type Service struct{}

var _ bashusecase.ProcessRunner = (*Service)(nil)

// outputSink serializes concurrent stdout and stderr delivery to the gRPC-safe callback.
type outputSink struct {
	mutex          sync.Mutex
	stdout         bytes.Buffer
	stderr         bytes.Buffer
	handleProgress bashusecase.ProgressHandler
	cancel         context.CancelCauseFunc
}

// streamWriter assigns one command writer to one output channel.
type streamWriter struct {
	sink   *outputSink
	stream bashusecase.Stream
}

// New creates a bash process service.
func New() *Service { return &Service{} }

// Run executes one bash command and captures output.
func (s *Service) Run(
	ctx context.Context,
	command string,
	handleProgress bashusecase.ProgressHandler,
) (bashusecase.ProcessResult, error) {
	if err := ctx.Err(); err != nil {
		return bashusecase.ProcessResult{}, fmt.Errorf("run bash: %w", err)
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return bashusecase.ProcessResult{}, fmt.Errorf("resolve bash: %w", err)
	}

	runContext, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	sink := &outputSink{
		mutex:          sync.Mutex{},
		stdout:         bytes.Buffer{},
		stderr:         bytes.Buffer{},
		handleProgress: handleProgress,
		cancel:         cancel,
	}
	process := exec.CommandContext( //nolint:gosec // The bash tool explicitly executes the model-provided command.
		context.WithoutCancel(runContext), bashPath, "-c", command,
	)
	process.SysProcAttr = &syscall.SysProcAttr{
		Chroot: "", Credential: nil, Ptrace: false, Setsid: false, Setpgid: true,
		Setctty: false, Noctty: false, Ctty: 0, Foreground: false, Pgid: 0,
	}
	process.Stdout = &streamWriter{sink: sink, stream: bashusecase.StreamStdout}
	process.Stderr = &streamWriter{sink: sink, stream: bashusecase.StreamStderr}
	startErr := process.Start()
	if startErr != nil {
		return bashusecase.ProcessResult{}, fmt.Errorf("start bash: %w", startErr)
	}

	processDone := make(chan struct{})
	killResult := make(chan error, 1)
	go watchCancellation(runContext, process.Process, processDone, killResult)
	waitErr := process.Wait()
	close(processDone)
	killErr := <-killResult
	result := sink.result(process.ProcessState.ExitCode())
	if cause := context.Cause(runContext); cause != nil {
		return bashusecase.ProcessResult{}, errors.Join(cause, killErr)
	}
	var exitErr *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &exitErr) {
		return bashusecase.ProcessResult{}, fmt.Errorf("wait for bash: %w", waitErr)
	}
	return result, nil
}

// Write captures and forwards one process-output fragment.
func (w *streamWriter) Write(content []byte) (int, error) {
	w.sink.mutex.Lock()
	defer w.sink.mutex.Unlock()
	buffer := &w.sink.stdout
	if w.stream == bashusecase.StreamStderr {
		buffer = &w.sink.stderr
	}
	_, _ = buffer.Write(content)
	if err := w.sink.handleProgress(w.stream, string(content)); err != nil {
		w.sink.cancel(err)
		return 0, err
	}
	return len(content), nil
}

// result snapshots complete output after the process and copy goroutines finish.
func (s *outputSink) result(exitCode int) bashusecase.ProcessResult {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return bashusecase.ProcessResult{Stdout: s.stdout.String(), Stderr: s.stderr.String(), ExitCode: exitCode}
}

// watchCancellation kills the process group immediately when execution is canceled.
func watchCancellation(ctx context.Context, process *os.Process, processDone <-chan struct{}, result chan<- error) {
	select {
	case <-ctx.Done():
		result <- killProcessGroup(process)
	case <-processDone:
		result <- nil
	}
}

// killProcessGroup falls back to the direct child only when group termination fails.
func killProcessGroup(process *os.Process) error {
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill bash process: %w", err)
	}
	return nil
}
