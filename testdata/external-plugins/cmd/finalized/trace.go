package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// traceEnvironment names a file that receives one line per handler invocation and tool call when set.
	traceEnvironment = "GLYPH_FINALIZED_TRACE"
	// tracePermission is the file mode for owner-only reading and writing.
	tracePermission = 0o600
)

// traceInvocation records the handler identifier and public payload of one handler invocation.
func (s *service) traceInvocation(handlerID string, invocation *extensionpb.FinalizedMessageInvocation) error {
	if os.Getenv(traceEnvironment) == "" {
		return nil
	}
	encoded, err := protojson.Marshal(invocation)
	if err != nil {
		return err
	}
	return s.trace(handlerID, encoded)
}

// trace appends one line with a name and a payload to the trace file when tracing is enabled.
func (s *service) trace(name string, payload []byte) (returnErr error) {
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
	_, err = fmt.Fprintf(file, "%s\t%s\n", name, payload)
	return err
}
