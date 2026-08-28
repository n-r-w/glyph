// Package credentials persists provider-owned opaque JSON payloads.
package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/codex"
)

const (
	credentialStoreVersion = 1
	maxCredentialStoreSize = 1 << 20
	credentialFileMode     = os.FileMode(0o600)
	credentialDirMode      = os.FileMode(0o700)
)

// credentialStore is the versioned Host-owned persistence envelope.
type credentialStore struct {
	Version   int                        `json:"version"`
	Providers map[string]json.RawMessage `json:"providers"`
}

// Service stores one provider payload in the shared credential file.
type Service struct {
	path       string
	providerID string
}

var _ codex.Credentials = (*Service)(nil)

// New creates a credential store view for one provider.
func New(path, providerID string) *Service {
	return &Service{path: path, providerID: providerID}
}

// Load returns a defensive copy of the provider's opaque JSON payload.
func (s *Service) Load() (payload []byte, found bool, err error) {
	validationErr := s.validateProviderID()
	if validationErr != nil {
		return nil, false, validationErr
	}
	store, found, err := s.readStore()
	if err != nil || !found {
		return nil, false, err
	}
	storedPayload, payloadFound := store.Providers[s.providerID]
	if !payloadFound {
		return nil, false, nil
	}
	return bytes.Clone(storedPayload), true, nil
}

// Save atomically replaces the provider's opaque JSON payload.
func (s *Service) Save(payload []byte) error {
	if err := s.validateProviderID(); err != nil {
		return err
	}
	if !json.Valid(payload) {
		return fmt.Errorf("save credentials for provider %q: payload is not valid JSON", s.providerID)
	}
	if err := s.ensureDirectory(); err != nil {
		return err
	}
	store, found, err := s.readStore()
	if err != nil {
		return err
	}
	if !found {
		store = credentialStore{Version: credentialStoreVersion, Providers: make(map[string]json.RawMessage)}
	}
	store.Providers[s.providerID] = append(json.RawMessage(nil), payload...)
	return s.writeStore(store)
}

// Delete atomically removes only this provider's payload.
func (s *Service) Delete() error {
	if err := s.validateProviderID(); err != nil {
		return err
	}
	store, found, err := s.readStore()
	if err != nil || !found {
		return err
	}
	if _, payloadFound := store.Providers[s.providerID]; !payloadFound {
		return nil
	}
	delete(store.Providers, s.providerID)
	return s.writeStore(store)
}

// validateProviderID prevents ambiguous empty keys in the shared provider map.
func (s *Service) validateProviderID() error {
	if s.providerID == "" {
		return errors.New("credential provider ID must not be empty")
	}
	return nil
}

// ensureDirectory enforces owner-only access before creating a temporary credential file.
func (s *Service) ensureDirectory() error {
	directory := filepath.Dir(filepath.Clean(s.path))
	if err := os.MkdirAll(directory, credentialDirMode); err != nil {
		return fmt.Errorf("create credential directory: %w", err)
	}
	permissionErr := os.Chmod(directory, credentialDirMode)
	if permissionErr != nil {
		return fmt.Errorf("enforce credential directory permissions: %w", permissionErr)
	}
	return nil
}

// readStore validates permissions, size, schema, and version before returning persisted data.
func (s *Service) readStore() (credentialStore, bool, error) {
	path := filepath.Clean(s.path)
	info, statErr := os.Stat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return credentialStore{Version: 0, Providers: nil}, false, nil
	}
	if statErr != nil {
		return credentialStore{}, false, fmt.Errorf("inspect credential store: %w", statErr)
	}
	if !info.Mode().IsRegular() {
		return credentialStore{}, false, fmt.Errorf("credential store %q is not a regular file", path)
	}
	permissionErr := os.Chmod(path, credentialFileMode)
	if permissionErr != nil {
		return credentialStore{}, false, fmt.Errorf("enforce credential store permissions: %w", permissionErr)
	}
	file, err := os.Open(path)
	if err != nil {
		return credentialStore{}, false, fmt.Errorf("open credential store: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxCredentialStoreSize+1))
	closeErr := file.Close()
	if readErr != nil {
		return credentialStore{}, false, fmt.Errorf("read credential store: %w", readErr)
	}
	if closeErr != nil {
		return credentialStore{}, false, fmt.Errorf("close credential store: %w", closeErr)
	}
	if len(data) > maxCredentialStoreSize {
		return credentialStore{}, false, fmt.Errorf("credential store exceeds %d bytes", maxCredentialStoreSize)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var store credentialStore
	decodeErr := decoder.Decode(&store)
	if decodeErr != nil {
		return credentialStore{}, false, fmt.Errorf("decode credential store: %w", decodeErr)
	}
	var extra any
	trailingErr := decoder.Decode(&extra)
	if !errors.Is(trailingErr, io.EOF) {
		if trailingErr == nil {
			return credentialStore{}, false, errors.New("decode trailing credential store data")
		}
		return credentialStore{}, false, fmt.Errorf("decode trailing credential store data: %w", trailingErr)
	}
	if store.Version != credentialStoreVersion {
		return credentialStore{}, false, fmt.Errorf(
			"credential store version %d is unsupported; expected %d",
			store.Version,
			credentialStoreVersion,
		)
	}
	if store.Providers == nil {
		store.Providers = make(map[string]json.RawMessage)
	}
	return store, true, nil
}

// writeStore replaces the complete envelope through an owner-only file in the same directory.
func (s *Service) writeStore(store credentialStore) error {
	data, encodeErr := json.Marshal(store)
	if encodeErr != nil {
		return fmt.Errorf("encode credential store: %w", encodeErr)
	}
	directory := filepath.Dir(filepath.Clean(s.path))
	temporary, temporaryErr := os.CreateTemp(directory, ".credentials-*.tmp")
	if temporaryErr != nil {
		return fmt.Errorf("create temporary credential store: %w", temporaryErr)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	permissionErr := temporary.Chmod(credentialFileMode)
	if permissionErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary credential store permissions: %w", permissionErr)
	}
	if _, writeErr := temporary.Write(data); writeErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary credential store: %w", writeErr)
	}
	syncErr := temporary.Sync()
	if syncErr != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary credential store: %w", syncErr)
	}
	closeErr := temporary.Close()
	if closeErr != nil {
		return fmt.Errorf("close temporary credential store: %w", closeErr)
	}
	renameErr := os.Rename(temporaryPath, filepath.Clean(s.path))
	if renameErr != nil {
		return fmt.Errorf("replace credential store: %w", renameErr)
	}
	removeTemporary = false
	return nil
}
