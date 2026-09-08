// Package bash executes project commands in isolated process groups.
package bash

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	// progressErr retains callback failures independently of whichever cancellation cause wins first.
	progressErr error
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

var _ io.Writer = (*streamWriter)(nil)

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
		progressErr:    nil,
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
	output, err := newProcessOutput(stdout, stderr)
	if err != nil {
		return bashusecase.ProcessResult{}, err
	}
	// File endpoints keep Cmd.Wait independent of the adapter's output workers.
	process.Stdout = output.stdout.writer
	process.Stderr = output.stderr.writer
	if startErr := process.Start(); startErr != nil {
		return bashusecase.ProcessResult{}, errors.Join(fmt.Errorf("start bash: %w", startErr), output.close())
	}
	writeCloseErr := output.closeWriters()
	if writeCloseErr != nil {
		cancel(writeCloseErr)
	}

	shellDone := make(chan struct{})
	outputDone := make(chan struct{})
	killResult := make(chan error, 1)
	go watchCancellation(runContext, process.Process, shellDone, outputDone, killResult)
	stdoutResult := make(chan error, 1)
	stderrResult := make(chan error, 1)
	go output.stdout.copy(cancel, stdoutResult)
	go output.stderr.copy(cancel, stderrResult)
	waitErr := process.Wait()
	close(shellDone)
	copyErr := errors.Join(<-stdoutResult, <-stderrResult)
	// A final UTF-8 callback can request cancellation, so keep monitoring through both flushes.
	progressErr := errors.Join(stdout.flush(), stderr.flush())
	close(outputDone)
	killErr := <-killResult
	cause := errors.Join(context.Cause(runContext), progressErr)
	result, outputErr := sink.result(process.ProcessState.ExitCode(), cause)
	if _, ok := errors.AsType[*exec.ExitError](waitErr); ok {
		waitErr = nil
	} else if waitErr != nil {
		waitErr = fmt.Errorf("wait for bash: %w", waitErr)
	}
	completionErr := errors.Join(cause, writeCloseErr, copyErr, killErr, waitErr, outputErr)
	if errors.Is(cause, context.Canceled) {
		return bashusecase.ProcessResult{}, errors.Join(completionErr, sink.discard())
	}
	return result, completionErr
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
			w.sink.failProgress(err)
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
		w.sink.failProgress(err)
		return err
	}
	return nil
}

// failProgress records a callback failure under the sink lock before requesting process cancellation.
func (s *outputSink) failProgress(err error) {
	s.progressErr = errors.Join(s.progressErr, err)
	s.cancel(err)
}

// result closes complete output and builds the bounded terminal text.
func (s *outputSink) result(exitCode int, cause error) (bashusecase.ProcessResult, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	output, truncation, err := s.output.finish(exitCode, cause)
	// Context cancellation cannot replace callback failures retained by the output owner.
	err = errors.Join(err, s.progressErr)
	return bashusecase.ProcessResult{Output: output, ExitCode: exitCode, Truncation: truncation}, err
}

// discard removes output that cannot be exposed after caller cancellation.
func (s *outputSink) discard() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.output.discard()
}

// watchCancellation remains active until output joins, even when the shell exits first.
func watchCancellation(
	ctx context.Context,
	process *os.Process,
	shellDone, outputDone <-chan struct{},
	result chan<- error,
) {
	select {
	case <-ctx.Done():
	case <-outputDone:
		if ctx.Err() == nil {
			result <- nil
			return
		}
	}
	immediateErr := killProcessGroup(process)
	// The shell cannot fork another child after its sole Cmd.Wait has returned.
	<-shellDone
	result <- errors.Join(immediateErr, killProcessGroup(process))
}

// killProcessGroup retains group errors even when the direct-child fallback succeeds.
func killProcessGroup(process *os.Process) error {
	groupErr := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if groupErr == nil || errors.Is(groupErr, syscall.ESRCH) {
		return nil
	}
	groupErr = fmt.Errorf("kill bash process group: %w", groupErr)
	if err := process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return errors.Join(groupErr, fmt.Errorf("kill bash process: %w", err))
	}
	return groupErr
}
