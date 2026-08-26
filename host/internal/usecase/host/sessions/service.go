// Package sessions owns the active session and its persisted lifecycle information.
package sessions

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
)

const formatVersion = 1

var lineBreaks = regexp.MustCompile(`[\r\n]+`)

// Service owns the process-active session.
type Service struct {
	// mutex makes active replacement and durable name updates atomic to readers.
	mutex sync.RWMutex
	// repository persists records for the canonical working directory.
	repository Repository
	// ids supplies independent session and entry identifiers.
	ids IDGenerator
	// clock supplies creation and update timestamps.
	clock Clock
	// workingDirectory binds created and resumed sessions to this process project.
	workingDirectory string
	// active is replaced only after create succeeds or resume fully validates its target.
	active LoadedSession
}

var _ sessioncontrol.ActiveSessions = (*Service)(nil)

// New creates an active-session service without performing storage I/O.
func New(repository Repository, ids IDGenerator, clock Clock, workingDirectory string) *Service {
	return &Service{
		mutex:            sync.RWMutex{},
		repository:       repository,
		ids:              ids,
		clock:            clock,
		workingDirectory: workingDirectory,
		active:           LoadedSession{},
	}
}

// Initialize prepares storage and creates one empty process-active session.
func (s *Service) Initialize(ctx context.Context) error {
	if err := s.repository.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize session repository: %w", err)
	}
	_, err := s.CreateActive(ctx)
	return err
}

// CreateActive replaces the active session with an empty session.
func (s *Service) CreateActive(_ context.Context) (session.Info, error) {
	id, err := s.ids.NewID()
	if err != nil {
		return session.Info{}, fmt.Errorf("create session ID: %w", err)
	}
	createdAt := s.clock.Now()
	loaded := LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               session.ID(id),
			CreatedAt:        createdAt,
			WorkingDirectory: s.workingDirectory,
		},
		StoragePath: "",
		Entries:     nil,
	}
	s.mutex.Lock()
	s.active = loaded
	s.mutex.Unlock()
	return infoFromLoaded(loaded), nil
}

// ResumeActive replaces the active session with a stored session.
func (s *Service) ResumeActive(ctx context.Context, id session.ID) (session.Info, error) {
	loaded, err := s.repository.Load(ctx, id)
	if err != nil {
		return session.Info{}, fmt.Errorf("load session: %w", err)
	}
	if loaded.Header.WorkingDirectory != s.workingDirectory {
		return session.Info{}, errors.New("session working directory does not match")
	}
	loaded = cloneLoaded(loaded)
	s.mutex.Lock()
	s.active = loaded
	s.mutex.Unlock()
	return infoFromLoaded(loaded), nil
}

// SetActiveName persists a normalized session name.
func (s *Service) SetActiveName(ctx context.Context, value string) (session.Info, error) {
	name := strings.TrimSpace(lineBreaks.ReplaceAllString(value, " "))
	if name == "" {
		return session.Info{}, session.ErrInvalidName
	}
	entryID, err := s.ids.NewID()
	if err != nil {
		return session.Info{}, fmt.Errorf("create session entry ID: %w", err)
	}
	entry := session.Entry{
		ID:          entryID,
		CreatedAt:   s.clock.Now(),
		Information: mo.Some(session.Information{Name: name}),
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	result, err := s.repository.Append(ctx, AppendCommand{
		Header:      s.active.Header,
		StoragePath: s.active.StoragePath,
		Entry:       entry,
	})
	if err != nil {
		return session.Info{}, fmt.Errorf("append session information: %w", err)
	}
	// The active snapshot advances only after the synchronized repository append succeeds.
	s.active.StoragePath = result.StoragePath
	s.active.Entries = append(s.active.Entries, entry)
	return infoFromLoaded(s.active), nil
}

// ListStored returns stored sessions ordered by update time and ID.
func (s *Service) ListStored(ctx context.Context) ([]session.Summary, error) {
	loaded, err := s.repository.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	result := make([]session.Summary, 0, len(loaded))
	for _, item := range loaded {
		result = append(result, session.Summary{
			Info:          infoFromLoaded(item),
			FirstUserText: mo.None[string](),
			TotalMessages: 0,
		})
	}
	sort.Slice(result, func(left int, right int) bool {
		if result[left].Info.UpdatedAt.Equal(result[right].Info.UpdatedAt) {
			return result[left].Info.ID < result[right].Info.ID
		}
		return result[left].Info.UpdatedAt.After(result[right].Info.UpdatedAt)
	})
	return result, nil
}

// ActiveInfo returns an independent active-session snapshot.
func (s *Service) ActiveInfo() session.Info {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return infoFromLoaded(s.active)
}

// infoFromLoaded derives name and update time from ordered records rather than filesystem metadata.
func infoFromLoaded(loaded LoadedSession) session.Info {
	name := mo.None[string]()
	updatedAt := loaded.Header.CreatedAt
	for _, entry := range loaded.Entries {
		if information, ok := entry.Information.Get(); ok {
			name = mo.Some(information.Name)
		}
		updatedAt = entry.CreatedAt
	}
	storagePath := mo.None[string]()
	if loaded.StoragePath != "" {
		storagePath = mo.Some(loaded.StoragePath)
	}
	return session.Info{
		ID:               loaded.Header.ID,
		Name:             name,
		WorkingDirectory: loaded.Header.WorkingDirectory,
		StoragePath:      storagePath,
		CreatedAt:        loaded.Header.CreatedAt,
		UpdatedAt:        updatedAt,
	}
}

// cloneLoaded prevents repository-owned entry slices from becoming mutable active state.
func cloneLoaded(value LoadedSession) LoadedSession {
	return LoadedSession{
		Header:      value.Header,
		StoragePath: value.StoragePath,
		Entries:     append([]session.Entry(nil), value.Entries...),
	}
}
