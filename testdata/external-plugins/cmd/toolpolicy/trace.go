package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// traceEnvironment names a file that receives one line per handler invocation and tool call when set.
	traceEnvironment = "GLYPH_TOOLPOLICY_TRACE"
	// tracePermission is the file mode for owner-only reading and writing.
	tracePermission = 0o600
)

// trace appends one line with a name and tab-separated payload fields to the trace file when tracing is enabled.
func (s *service) trace(name string, fields ...[]byte) (returnErr error) {
	path := os.Getenv(traceEnvironment)
	if path == "" {
		return nil
	}
	s.traceMutex.Lock()
	defer s.traceMutex.Unlock()
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	file, err := root.OpenFile(filepath.Base(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, tracePermission)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, file.Close()) }()
	payload := make([]string, len(fields))
	for index, field := range fields {
		payload[index] = string(field)
	}
	_, err = fmt.Fprintf(file, "%s\t%s\n", name, strings.Join(payload, "\t"))
	return err
}
