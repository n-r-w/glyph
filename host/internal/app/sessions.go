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
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

// sessionComposition keeps one active-session service and one shared operation gate per Host process.
type sessionComposition struct {
	// active owns the session snapshot initialized before any client starts.
	active *hostsessions.Service
	// control exposes lifecycle operations to client transports.
	control *sessioncontrol.Service
	// gate serializes session replacement with agent execution across all client paths.
	gate *operationgate.Service
}

// newSessionComposition prepares project storage before providers, clients, or agent runs start.
func newSessionComposition(ctx context.Context, paths persistence.Paths) (sessionComposition, error) {
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
	active := hostsessions.New(repository, sessionruntime.CryptoIDGenerator{}, sessionruntime.SystemClock{}, canonical)
	if initializeErr := active.Initialize(ctx); initializeErr != nil {
		return sessionComposition{}, initializeErr
	}
	gate := operationgate.New()
	return sessionComposition{
		active:  active,
		control: sessioncontrol.New(active, gate),
		gate:    gate,
	}, nil
}
