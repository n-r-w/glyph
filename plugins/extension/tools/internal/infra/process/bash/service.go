// Package bash executes project commands in isolated process groups.
package bash

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unicode/utf8"

	bashusecase "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
)

// Service runs bash commands.
type Service struct{}

var _ bashusecase.ProcessRunner = (*Service)(nil)

// outputSink serializes concurrent stdout and stderr delivery to the gRPC-safe callback.
type outputSink struct {
	// mutex serializes output and progress delivery.
	mutex sync.Mutex
	// output retains bounded and complete command output.
	output *outputStore
	// handleProgress receives ordered command output fragments.
	handleProgress bashusecase.ProgressHandler
	// cancel stops execution after output handling failure.
	cancel context.CancelCauseFunc
}

// streamWriter assigns one command writer to one output channel.
type streamWriter struct {
	// sink owns shared command output state.
	sink *outputSink
	// stream identifies standard output or standard error.
	stream bashusecase.Stream
	// pending retains an incomplete UTF-8 sequence.
	pending []byte
}

// New creates a bash process service.
func New() *Service { return &Service{} }

// Run executes one bash command and captures output.
func (s *Service) Run(
	ctx context.Context,
	command string,
	handleProgress bashusecase.ProgressHandler,
) (bashusecase.ProcessResult, error) {
	if ctx.Err() != nil {
		return bashusecase.ProcessResult{}, fmt.Errorf("run bash: %w", context.Cause(ctx))
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return bashusecase.ProcessResult{}, fmt.Errorf("resolve bash: %w", err)
	}

	runContext, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	sink := &outputSink{
		mutex:          sync.Mutex{},
		output:         newOutputStore(),
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
	stdout := &streamWriter{sink: sink, stream: bashusecase.StreamStdout, pending: nil}
	stderr := &streamWriter{sink: sink, stream: bashusecase.StreamStderr, pending: nil}
	process.Stdout = stdout
	process.Stderr = stderr
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
	progressErr := errors.Join(stdout.flush(), stderr.flush())
	cause := errors.Join(context.Cause(runContext), progressErr)
	result, outputErr := sink.result(process.ProcessState.ExitCode(), cause)
	if cause != nil {
		if errors.Is(cause, context.Canceled) {
			discardErr := sink.discard()
			return bashusecase.ProcessResult{}, errors.Join(cause, killErr, outputErr, discardErr)
		}
		return result, errors.Join(cause, killErr, outputErr)
	}
	if outputErr != nil {
		return result, outputErr
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
	if err := w.sink.output.append(content); err != nil {
		w.sink.cancel(err)
		return 0, err
	}
	progress := w.decode(content)
	if progress != "" {
		w.sink.output.appendText(progress)
		if err := w.sink.handleProgress(w.stream, progress); err != nil {
			w.sink.cancel(err)
			return 0, err
		}
	}
	return len(content), nil
}

// decode carries incomplete UTF-8 between raw process fragments.
func (w *streamWriter) decode(content []byte) string {
	data := make([]byte, 0, len(w.pending)+len(content))
	data = append(data, w.pending...)
	data = append(data, content...)
	w.pending = w.pending[:0]
	visible := make([]byte, 0, len(data))
	for len(data) > 0 {
		if !utf8.FullRune(data) {
			w.pending = append(w.pending, data...)
			break
		}
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			visible = append(visible, '?')
			data = data[1:]
			continue
		}
		visible = append(visible, data[:size]...)
		data = data[size:]
	}
	return string(visible)
}

// flush replaces an incomplete trailing rune after the process stream closes.
func (w *streamWriter) flush() error {
	w.sink.mutex.Lock()
	defer w.sink.mutex.Unlock()
	if len(w.pending) == 0 {
		return nil
	}
	w.pending = w.pending[:0]
	w.sink.output.appendText("?")
	if err := w.sink.handleProgress(w.stream, "?"); err != nil {
		w.sink.cancel(err)
		return err
	}
	return nil
}

// result closes complete output and builds the bounded terminal text.
func (s *outputSink) result(exitCode int, cause error) (bashusecase.ProcessResult, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	output, truncation, err := s.output.finish(exitCode, cause)
	return bashusecase.ProcessResult{Output: output, ExitCode: exitCode, Truncation: truncation}, err
}

// discard removes output that cannot be exposed after caller cancellation.
func (s *outputSink) discard() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.output.discard()
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
