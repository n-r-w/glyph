// Package sessions owns the active session and its persisted lifecycle information.
package sessions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

const formatVersion = 2

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

var (
	_ sessioncontrol.ActiveSessions = (*Service)(nil)
	_ sessiontree.ActiveSession     = (*Service)(nil)
	_ agentrun.HistoryStore         = (*Service)(nil)
)

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
	tree, treeErr := session.NewTree(nil, mo.None[string](), nil)
	if treeErr != nil {
		return session.Replacement{}, fmt.Errorf("create empty session tree: %w", treeErr)
	}
	loaded := LoadedSession{
		Header: session.Header{
			Version:          formatVersion,
			ID:               session.ID(id),
			CreatedAt:        createdAt,
			WorkingDirectory: s.workingDirectory,
		},
		StoragePath:          "",
		Tree:                 tree,
		Information:          mo.None[session.Information](),
		InformationUpdatedAt: mo.None[time.Time](),
	}
	s.mutex.Lock()
	s.active = loaded
	s.history = nil
	// Active replacement creates a new process-local write state independent from the replaced session.
	s.writeUnavailable = false
	replacement := s.active.Replacement()
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
	loaded = loaded.Clone()
	history := historyFromEntries(loaded.Tree.ActiveBranch())
	s.active = loaded
	s.history = history
	// Successful validation and replacement are the only resume path that restores mutation access.
	s.writeUnavailable = false
	return s.active.Replacement(), nil
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
	updatedAt := s.clock.Now()
	result, err := s.repository.Apply(ctx, ApplyCommand{
		Header: s.active.Header, StoragePath: s.active.StoragePath,
		Mutation: Mutation{
			Entry: mo.None[session.Entry](), Navigation: mo.None[NavigationMutation](),
			Label: mo.None[LabelMutation](), SessionInformation: mo.Some(SessionInformationMutation{
				Name: name, CreatedAt: updatedAt,
			}),
		},
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationName, s.active.Header.ID, err)
		// Keep the last durable snapshot readable while blocking later process-local mutations.
		s.writeUnavailable = true
		return session.Info{}, fmt.Errorf("%w: append session information: %w", session.ErrPersistenceUnavailable, err)
	}
	// The active snapshot advances only after the synchronized repository mutation succeeds.
	s.active.StoragePath = result.StoragePath
	s.active.Information = mo.Some(session.Information{Name: name})
	s.active.InformationUpdatedAt = mo.Some(updatedAt)
	return s.active.Info(), nil
}

// ListStored returns stored sessions ordered by update time and ID.
func (s *Service) ListStored(ctx context.Context) ([]session.Summary, error) {
	loaded, err := s.repository.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	result := make([]session.Summary, 0, len(loaded))
	for itemIndex := range loaded {
		item := &loaded[itemIndex]
		activeBranch := item.Tree.ActiveBranch()
		counts := countSessionEntries(item.Tree.Entries())
		firstUserText := mo.None[string]()
		for entryIndex := range activeBranch {
			entry := &activeBranch[entryIndex]
			if user, present := entry.User.Get(); present && firstUserText.IsNone() {
				text := strings.TrimSpace(lineBreaks.ReplaceAllString(user.Text(""), " "))
				if text != "" {
					firstUserText = mo.Some(text)
				}
			}
		}
		result = append(result, session.Summary{
			Info:          item.Info(),
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
	return s.active.Info()
}

// ActiveEntries returns immutable active-branch records in root-first order.
func (s *Service) ActiveEntries() []session.Entry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return cloneEntries(s.active.Tree.ActiveBranch())
}

// Tree returns a defensive snapshot of the complete active session tree.
func (s *Service) Tree() session.Tree {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.active.Tree.Clone()
}

// ActiveStatistics derives counts and complete token totals from durable entries.
func (s *Service) ActiveStatistics() session.Statistics {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return statisticsFromEntries(s.active.Tree.Entries())
}

// ActiveInformation returns metadata and statistics from one locked active-session snapshot.
func (s *Service) ActiveInformation() session.InformationSnapshot {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	info := s.active.Info()
	return session.InformationSnapshot{
		Info:       info,
		Statistics: statisticsFromEntries(s.active.Tree.Entries()),
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
	owned, err := history.ValidatedClone()
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
	appendErr := s.appendEntryLocked(ctx, projection)
	if appendErr != nil {
		return appendErr
	}
	s.history = append(s.history, owned)
	return nil
}

// AppendExtension persists one model-hidden extension entry on the current active branch.
func (s *Service) AppendExtension(ctx context.Context, extension session.ExtensionEnvelope) error {
	owned := extension
	owned.Data = bytes.Clone(extension.Data)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.appendEntryLocked(ctx, session.Entry{
		ID: "", ParentID: mo.None[string](), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(owned),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	})
}

// appendEntryLocked persists one candidate child and publishes it only after synchronization.
func (s *Service) appendEntryLocked(ctx context.Context, entry session.Entry) error {
	if s.writeUnavailable {
		return agentrun.ErrPersistenceUnavailable
	}
	entryID, err := s.ids.NewID()
	if err != nil {
		return fmt.Errorf("create session entry ID: %w", err)
	}
	entry.ID = entryID
	entry.ParentID = s.active.Tree.ActiveLeafID()
	entry.CreatedAt = s.clock.Now()
	candidateTree := s.active.Tree.Clone()
	if err = candidateTree.Add(entry); err != nil {
		return fmt.Errorf("validate session tree entry: %w", err)
	}
	result, err := s.repository.Apply(ctx, ApplyCommand{
		Header: s.active.Header, StoragePath: s.active.StoragePath,
		Mutation: Mutation{
			Entry: mo.Some(entry), Navigation: mo.None[NavigationMutation](),
			Label: mo.None[LabelMutation](), SessionInformation: mo.None[SessionInformationMutation](),
		},
	})
	if err != nil {
		logPersistenceFailure(ctx, persistenceOperationHistory, s.active.Header.ID, err)
		// Keep the last durable snapshot readable while blocking later process-local mutations.
		s.writeUnavailable = true
		return fmt.Errorf("%w: append session entry: %w", agentrun.ErrPersistenceUnavailable, err)
	}
	// Publish active ownership only after the repository append is synchronized.
	s.active.StoragePath = result.StoragePath
	s.active.Tree = candidateTree
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
