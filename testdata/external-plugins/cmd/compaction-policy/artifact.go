// Package main supplies a public-only reference compaction policy.
package main

import (
	"errors"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// writePolicyArtifact writes one optional public protobuf artifact with owned permissions.
func writePolicyArtifact(path string, message proto.Message) error {
	if path == "" {
		return nil
	}
	encoded, err := protojson.Marshal(message)
	if err != nil {
		return err
	}
	return writePolicyArtifactBytes(path, encoded)
}

// writePolicyArtifactBytes stores a fixture artifact under its supplied directory.
func writePolicyArtifactBytes(path string, encoded []byte) error {
	// policyArtifactPermission is the file mode for owner-only reading and writing.
	const policyArtifactPermission = 0o600
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	writeErr := root.WriteFile(filepath.Base(path), encoded, policyArtifactPermission)
	return errors.Join(writeErr, root.Close())
}
