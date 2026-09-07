//go:build integration

package terminal

import (
	"bytes"
	"io"
	"sync"
)

// notifyingWriter records output and signals the first completed render.
type notifyingWriter struct {
	// mutex protects concurrent renderer writes and assertions.
	mutex sync.Mutex
	// buffer retains rendered bytes.
	buffer bytes.Buffer
	// written signals completion of the first render.
	written chan struct{}
}

var _ io.Writer = (*notifyingWriter)(nil)

// newNotifyingWriter creates an output recorder with a first-write signal.
func newNotifyingWriter() *notifyingWriter {
	return &notifyingWriter{
		written: make(chan struct{}, 1),
		mutex:   sync.Mutex{},
		buffer:  bytes.Buffer{},
	}
}

// Write records rendered bytes and closes the first-write signal once.
func (writer *notifyingWriter) Write(content []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	select {
	case writer.written <- struct{}{}:
	default:
	}
	return writer.buffer.Write(content)
}

// String returns a stable copy of rendered bytes.
func (writer *notifyingWriter) String() string {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.buffer.String()
}
