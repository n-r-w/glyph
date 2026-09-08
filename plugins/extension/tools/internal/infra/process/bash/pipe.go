package bash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// processOutput owns both command pipes independently of shell completion.
type processOutput struct {
	// stdout copies the command's standard output.
	stdout *outputPipe
	// stderr copies the command's standard error.
	stderr *outputPipe
}

// closeWriters releases parent copies after Start transfers the endpoints to the child.
func (o *processOutput) closeWriters() error {
	return errors.Join(o.stdout.writer.Close(), o.stderr.writer.Close())
}

// close releases all endpoints when the process cannot start.
func (o *processOutput) close() error {
	return errors.Join(o.closeWriters(), o.stdout.reader.Close(), o.stderr.reader.Close())
}

// outputPipe owns one reader, its child-facing writer, and its text projection.
type outputPipe struct {
	// reader supplies raw command bytes to the copy worker.
	reader *os.File
	// writer is passed to the child and closed in the parent after Start.
	writer *os.File
	// destination retains raw bytes and delivers serialized UTF-8 progress.
	destination *streamWriter
}

// copy drains one stream and closes its reader before reporting completion.
func (p *outputPipe) copy(cancel context.CancelCauseFunc, result chan<- error) {
	_, copyErr := io.Copy(p.destination, p.reader)
	if copyErr != nil {
		copyErr = fmt.Errorf("copy bash output: %w", copyErr)
		cancel(copyErr)
	}
	closeErr := p.reader.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close bash output pipe: %w", closeErr)
		cancel(closeErr)
	}
	result <- errors.Join(copyErr, closeErr)
}

// newProcessOutput creates both pipes and closes partial allocations on failure.
func newProcessOutput(stdout, stderr *streamWriter) (*processOutput, error) {
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create bash stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("create bash stderr pipe: %w", err),
			stdoutReader.Close(),
			stdoutWriter.Close(),
		)
	}
	return &processOutput{
		stdout: &outputPipe{reader: stdoutReader, writer: stdoutWriter, destination: stdout},
		stderr: &outputPipe{reader: stderrReader, writer: stderrWriter, destination: stderr},
	}, nil
}
