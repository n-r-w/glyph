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
	// traceEnvironment names a file that receives one line per invocation when set.
	traceEnvironment = "GLYPH_INPUT_TRACE"
	// tracePermission is the file mode for owner-only reading and writing.
	tracePermission = 0o600
)

// trace appends the handler identifier and public payload of one invocation to the trace file.
func (s *service) trace(handlerID string, input *extensionpb.UserInputInvocation) (returnErr error) {
	path := os.Getenv(traceEnvironment)
	if path == "" {
		return nil
	}
	encoded, err := protojson.Marshal(input)
	if err != nil {
		return err
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
	_, err = fmt.Fprintf(file, "%s\t%s\n", handlerID, encoded)
	return err
}
