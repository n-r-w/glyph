package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
	"github.com/n-r-w/glyph/host/internal/infra/sessionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/operationgate"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// sessionComposition keeps one active-session service and one shared operation gate per Host process.
type sessionComposition struct {
	// active owns the session snapshot initialized before any client starts.
	active *hostsessions.Service
	// gate serializes session replacement with agent execution across all client paths.
	gate *operationgate.Service
	// tree owns session-tree handler registrations and navigation policy.
	tree *sessiontree.Service
}

// newSessionComposition prepares project storage before providers, clients, or agent runs start.
func newSessionComposition(
	ctx context.Context,
	paths persistence.Paths,
	handlerRuntime sessiontree.Runtime,
) (sessionComposition, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return sessionComposition{}, fmt.Errorf("get working directory: %w", err)
	}
	canonical, err := sessionstore.CanonicalWorkingDirectory(workingDirectory)
	if err != nil {
		return sessionComposition{}, err
	}
	repository := sessionstore.New(
		filepath.Join(paths.Directory, "sessions"), canonical, sessionfilesystem.New(),
	)
	active := hostsessions.New(
		repository, sessionruntime.CryptoIDGenerator{}, sessionruntime.SystemClock{}, nil, canonical,
	)
	if initializeErr := active.Initialize(ctx); initializeErr != nil {
		return sessionComposition{}, initializeErr
	}
	gate := operationgate.New()
	tree := sessiontree.New(active, nil, handlerRuntime)
	return sessionComposition{active: active, gate: gate, tree: tree}, nil
}
