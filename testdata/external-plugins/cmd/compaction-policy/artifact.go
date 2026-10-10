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
	// policyArtifactPermission restricts captured input to its fixture owner.
	const policyArtifactPermission = 0o600

	if path == "" {
		return nil
	}
	// encoded contains the public protobuf JSON artifact.
	encoded, err := protojson.Marshal(message)
	if err != nil {
		return err
	}
	// root owns the configured artifact directory.
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	// writeErr retains the artifact write cause through close.
	writeErr := root.WriteFile(filepath.Base(path), encoded, policyArtifactPermission)
	return errors.Join(writeErr, root.Close())
}
