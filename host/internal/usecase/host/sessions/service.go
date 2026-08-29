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

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
)

const formatVersion = 1

var _ agentrun.HistoryStore = (*Service)(nil)

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
	// pricing resolves rates by configured provider and requested model.
	pricing PricingCatalog
	// workingDirectory binds created and resumed sessions to this process project.
	workingDirectory string
	// active contains durable session records and public metadata.
	active LoadedSession
	// history is the complete provider-neutral in-process history owned by this store.
	history []agent.HistoryEntry
	// writeUnavailable blocks mutations after this process observes a persistence failure.
	writeUnavailable bool
}

var _ sessioncontrol.ActiveSessions = (*Service)(nil)

// New creates an active-session service without performing storage I/O.
func New(
	repository Repository,
	ids IDGenerator,
	clock Clock,
	pricing PricingCatalog,
	workingDirectory string,
) *Service {
	return &Service{
		mutex:            sync.RWMutex{},
		repository:       repository,
		ids:              ids,
		clock:            clock,
		pricing:          pricing,
		workingDirectory: workingDirectory,
		active:           LoadedSession{},
		history:          nil,
		writeUnavailable: false,
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
func (s *Service) CreateActive(_ context.Context) (session.Replacement, error) {
	id, err := s.ids.NewID()
	if err != nil {
		return session.Replacement{}, fmt.Errorf("create session ID: %w", err)
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
	s.history = nil
	// Active replacement creates a new process-local write state independent from the replaced session.
	s.writeUnavailable = false
	replacement := replacementFromLoaded(s.active)
	s.mutex.Unlock()
	return replacement, nil
}

// ResumeActive replaces the active session with a stored session.
func (s *Service) ResumeActive(ctx context.Context, id session.ID) (session.Replacement, error) {
	// The lock spans load and replacement so stale loaded state cannot overwrite a completed append or name change.
	s.mutex.Lock()
	defer s.mutex.Unlock()

	loaded, err := s.repository.Load(ctx, id)
	if err != nil {
		if errors.Is(err, session.ErrPersistenceUnavailable) {
			logPersistenceFailure(ctx, persistenceOperationResume, "", err)
		}
		return session.Replacement{}, fmt.Errorf("load session: %w", err)
	}
	if loaded.Header.WorkingDirectory != s.workingDirectory {
		return session.Replacement{}, errors.New("session working directory does not match")
	}
	loaded = cloneLoaded(loaded)
	history := historyFromEntries(loaded.Entries)
	s.active = loaded
	s.history = history
	// Successful validation and replacement are the only resume path that restores mutation access.
	s.writeUnavailable = false
	return replacementFromLoaded(s.active), nil
}

// SetActiveName persists a normalized session name.
func (s *Service) SetActiveName(ctx context.Context, value string) (session.Info, error) {
	name := strings.TrimSpace(lineBreaks.ReplaceAllString(value, " "))
	if name == "" {
		return session.Info{}, session.ErrInvalidName
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.writeUnavailable {
		return session.Info{}, session.ErrPersistenceUnavailable
	}
	entryID, err := s.ids.NewID()
	if err != nil {
		return session.Info{}, fmt.Errorf("create session entry ID: %w", err)
	}
	entry := session.Entry{
		User:        mo.None[session.UserMessage](),
		Model:       mo.None[session.ModelResponse](),
		ToolResult:  mo.None[session.ToolResult](),
		Extension:   mo.None[session.ExtensionEnvelope](),
		ID:          entryID,
		CreatedAt:   s.clock.Now(),
		Information: mo.Some(session.Information{Name: name}), EstimatedCost: mo.None[session.EstimatedCost](),
	}

	result, err := s.repository.Append(ctx, AppendCommand{
		Header:      s.active.Header,
		StoragePath: s.active.StoragePath,
		Entry:       entry,
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationName, s.active.Header.ID, err)
		// Keep the last durable snapshot readable while blocking later process-local mutations.
		s.writeUnavailable = true
		return session.Info{}, fmt.Errorf("%w: append session information: %w", session.ErrPersistenceUnavailable, err)
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
		counts := countSessionEntries(item.Entries)
		firstUserText := mo.None[string]()
		for entryIndex := range item.Entries {
			entry := &item.Entries[entryIndex]
			if user, present := entry.User.Get(); present && firstUserText.IsNone() {
				text := strings.TrimSpace(lineBreaks.ReplaceAllString(publicUserText(user), " "))
				if text != "" {
					firstUserText = mo.Some(text)
				}
			}
		}
		result = append(result, session.Summary{
			Info:          infoFromLoaded(item),
			FirstUserText: firstUserText,
			TotalMessages: counts.totalMessages,
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

// ActiveEntries returns immutable active-session records in stored order.
func (s *Service) ActiveEntries() []session.Entry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return cloneEntries(s.active.Entries)
}

// ActiveStatistics derives counts and complete token totals from durable entries.
func (s *Service) ActiveStatistics() session.Statistics {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return statisticsFromEntries(s.active.Entries)
}

// ActiveInformation returns metadata and statistics from one locked active-session snapshot.
func (s *Service) ActiveInformation() session.InformationSnapshot {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	info := infoFromLoaded(s.active)
	return session.InformationSnapshot{
		Info:       info,
		Statistics: statisticsFromEntries(s.active.Entries),
	}
}

// Snapshot returns the provider-neutral history owned by the active session.
func (s *Service) Snapshot() []agent.HistoryEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return cloneHistory(s.history)
}

// Append transfers one history entry to active ownership. Complete valid user, terminal model,
// and tool-result history is durable when Append succeeds.
func (s *Service) Append(ctx context.Context, history agent.HistoryEntry) error {
	owned, err := cloneValidatedHistoryEntry(history)
	if err != nil {
		return err
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.writeUnavailable {
		return agentrun.ErrPersistenceUnavailable
	}
	projection, durable, err := terminalContinuationEntry(owned)
	if err != nil {
		return err
	}
	if !durable {
		// Unsupported partial model responses remain complete but process-local.
		s.history = append(s.history, owned)
		return nil
	}
	if response, modelPresent := projection.Model.Get(); modelPresent {
		projection.EstimatedCost = s.estimatedCost(response)
	}
	entryID, err := s.ids.NewID()
	if err != nil {
		return fmt.Errorf("create session entry ID: %w", err)
	}
	projection.ID = entryID
	projection.CreatedAt = s.clock.Now()
	result, err := s.repository.Append(ctx, AppendCommand{
		Header:      s.active.Header,
		StoragePath: s.active.StoragePath,
		Entry:       projection,
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationHistory, s.active.Header.ID, err)
		// Keep the last durable snapshot readable while blocking later process-local mutations.
		s.writeUnavailable = true
		return fmt.Errorf("%w: append session history: %w", agentrun.ErrPersistenceUnavailable, err)
	}
	// Publish active ownership only after the repository append is synchronized.
	// Callers can start dependent work after Append returns.
	s.active.StoragePath = result.StoragePath
	s.active.Entries = append(s.active.Entries, projection)
	s.history = append(s.history, owned)
	return nil
}

// estimatedCost calculates one persisted request cost from disjoint normalized token buckets.
func (s *Service) estimatedCost(response model.Response) mo.Option[session.EstimatedCost] {
	usage, usagePresent := response.Usage.Get()
	providerID, providerPresent := response.Provider.Get()
	modelID, modelPresent := response.Model.Get()
	if !usagePresent || !providerPresent || !modelPresent {
		return mo.None[session.EstimatedCost]()
	}
	pricing, pricingPresent := s.pricing.Pricing(providerID, modelID).Get()
	if !pricingPresent {
		return mo.None[session.EstimatedCost]()
	}
	rates := model.PricingTier{
		InputTokensAbove: 0,
		Input:            pricing.Input, Output: pricing.Output, CacheRead: pricing.CacheRead, CacheWrite: pricing.CacheWrite,
	}
	requestInput := usage.InputTokens + usage.CachedInputTokens + usage.CacheWriteTokens
	for tierIndex := range pricing.Tiers {
		if requestInput > pricing.Tiers[tierIndex].InputTokensAbove {
			rates = pricing.Tiers[tierIndex]
		}
	}
	const tokensPerMillion = 1_000_000
	cost := session.EstimatedCost{
		Input:      float64(usage.InputTokens) * rates.Input / tokensPerMillion,
		Output:     float64(usage.OutputTokens) * rates.Output / tokensPerMillion,
		CacheRead:  float64(usage.CachedInputTokens) * rates.CacheRead / tokensPerMillion,
		CacheWrite: float64(usage.CacheWriteTokens) * rates.CacheWrite / tokensPerMillion,
		Total:      0,
	}
	// Output already contains the reasoning subset, so no separate reasoning charge is added.
	cost.Total = cost.Input + cost.Output + cost.CacheRead + cost.CacheWrite
	return mo.Some(cost)
}
