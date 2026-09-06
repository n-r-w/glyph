// Package sessions owns the active session and its persisted lifecycle information.
package sessions

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessioncontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

const formatVersion = 2

var lineBreaks = regexp.MustCompile(`[\r\n]+`)

// Service owns the process-active session.
type Service struct {
	// mutex makes session commits and their ordered client publication atomic to readers and competing mutations.
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
	// contextIdentity publishes immutable incarnation state without waiting for storage I/O locks.
	contextIdentity atomic.Pointer[extensioncontext.SessionIdentity]
	// history owns provider-neutral values and their ordinary client visibility.
	history []storedHistoryEntry
	// writeUnavailable blocks mutations after this process observes a persistence failure.
	writeUnavailable bool
}

var (
	_ sessioncontrol.ActiveSessions = (*Service)(nil)
	_ sessiontree.ActiveSession     = (*Service)(nil)
	_ agentrun.HistoryStore         = (*Service)(nil)
	_ extensioncontext.SessionState = (*Service)(nil)
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
		contextIdentity:  atomic.Pointer[extensioncontext.SessionIdentity]{},
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
	s.publishContextIdentityLocked()
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
	branch := loaded.Tree.ActiveBranch()
	history := storedHistoryFromEntries(branch)
	s.active = loaded
	s.publishContextIdentityLocked()
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

// SessionID returns the active session identifier for navigation handlers.
func (s *Service) SessionID() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return string(s.active.Header.ID)
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
	return cloneStoredHistory(s.history, false)
}

// ClientSnapshot returns ordinary transcript history with hidden extension messages excluded.
func (s *Service) ClientSnapshot() []agent.HistoryEntry {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return cloneStoredHistory(s.history, true)
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
		s.history = append(s.history, storedHistoryEntry{value: owned, clientVisible: true})
		return nil
	}
	if response, modelPresent := projection.Model.Get(); modelPresent {
		projection.EstimatedCost = s.estimatedCost(response)
	}
	_, appendErr := s.appendEntryLocked(ctx, projection)
	if appendErr != nil {
		return appendErr
	}
	s.history = append(s.history, storedHistoryEntry{value: owned, clientVisible: true})
	return nil
}

// AppendExtension persists one model-hidden entry only for the expected active-session incarnation.
func (s *Service) AppendExtension(
	ctx context.Context,
	expected extensioncontext.SessionIdentity,
	extension session.ExtensionEnvelope,
	commitGuard extensioncontext.ContextCommitGuard,
) (session.Entry, error) {
	owned := extension.Clone()
	if owned.ExtensionID == "" || owned.EntryType == "" || !jsontext.Value(owned.Data).IsValid() {
		return session.Entry{}, errors.New("invalid extension entry")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := s.validateExpectedSessionLocked(ctx, expected); err != nil {
		return session.Entry{}, err
	}
	if commitGuard == nil {
		return session.Entry{}, errors.New("runtime commit validation is required")
	}
	releaseCommit, err := commitGuard()
	if err != nil {
		return session.Entry{}, fmt.Errorf("validate extension runtime before session commit: %w", err)
	}
	defer releaseCommit()
	entry := session.Entry{
		ID: "", ParentID: mo.None[string](), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(owned),
		ExtensionMessage: mo.None[session.ExtensionMessage](), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	committed, err := s.appendEntryLocked(ctx, entry)
	if err != nil {
		if errors.Is(err, agentrun.ErrPersistenceUnavailable) {
			return session.Entry{}, fmt.Errorf("%w: %w", session.ErrPersistenceUnavailable, err)
		}
		return session.Entry{}, err
	}
	return committed, nil
}

// AppendExtensionMessage persists and enqueues one message under the session lock, then waits without commit locks.
func (s *Service) AppendExtensionMessage(
	ctx context.Context,
	expected extensioncontext.SessionIdentity,
	message session.ExtensionMessage,
	commitGuard extensioncontext.ContextCommitGuard,
	publisher func(session.Entry) (wait func(context.Context) error, err error),
) (session.Entry, error) {
	if message.ExtensionID == "" || message.EntryType == "" ||
		message.Visibility != session.ClientVisibilityVisible && message.Visibility != session.ClientVisibilityHidden {
		return session.Entry{}, errors.New("invalid extension message")
	}
	if commitGuard == nil {
		return session.Entry{}, errors.New("runtime commit validation is required")
	}
	s.mutex.Lock()
	if err := s.validateExpectedSessionLocked(ctx, expected); err != nil {
		s.mutex.Unlock()
		return session.Entry{}, err
	}
	releaseCommit, err := commitGuard()
	if err != nil {
		s.mutex.Unlock()
		return session.Entry{}, fmt.Errorf("validate extension runtime before session commit: %w", err)
	}
	entry := session.Entry{
		ID: "", ParentID: mo.None[string](), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.Some(message), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	committed, err := s.appendEntryLocked(ctx, entry)
	if err != nil {
		releaseCommit()
		s.mutex.Unlock()
		if errors.Is(err, agentrun.ErrPersistenceUnavailable) {
			return session.Entry{}, fmt.Errorf("%w: %w", session.ErrPersistenceUnavailable, err)
		}
		return session.Entry{}, err
	}
	s.history = append(s.history, storedHistoryFromEntries([]session.Entry{committed})...)
	var wait func(context.Context) error
	var publishErr error
	if publisher == nil {
		publishErr = errors.New("extension message publisher is not bound")
	} else {
		wait, publishErr = publisher(committed)
	}
	releaseCommit()
	s.mutex.Unlock()
	if publishErr != nil {
		return committed, fmt.Errorf("publish committed extension message: %w", publishErr)
	}
	if wait == nil {
		return committed, errors.New("publish committed extension message: delivery wait is required")
	}
	if waitErr := wait(ctx); waitErr != nil {
		return committed, fmt.Errorf("deliver committed extension message: %w", waitErr)
	}
	return committed, nil
}

// ExtensionState returns the caller extension's entries from one locked active-branch snapshot.
func (s *Service) ExtensionState(
	ctx context.Context,
	expected extensioncontext.SessionIdentity,
	extensionID string,
) (extensioncontext.SessionSnapshot, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if err := s.validateExpectedSessionLocked(ctx, expected); err != nil {
		return extensioncontext.SessionSnapshot{}, err
	}
	entries := make([]session.Entry, 0)
	activeBranch := s.active.Tree.ActiveBranch()
	for entryIndex := range activeBranch {
		entry := &activeBranch[entryIndex]
		extension, hiddenPresent := entry.Extension.Get()
		message, messagePresent := entry.ExtensionMessage.Get()
		if hiddenPresent && extension.ExtensionID == extensionID ||
			messagePresent && message.ExtensionID == extensionID {
			entries = append(entries, entry.Clone())
		}
	}
	return extensioncontext.SessionSnapshot{
		SessionID: s.active.Header.ID, ActiveLeafID: s.active.Tree.ActiveLeafID(), Entries: entries,
	}, nil
}

// validateExpectedSessionLocked rejects cancellation and every replaced active-session incarnation.
func (s *Service) validateExpectedSessionLocked(ctx context.Context, expected extensioncontext.SessionIdentity) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("access extension session state: %w", err)
	}
	current := s.ContextSession()
	if current.ID != expected.ID || current.Incarnation != expected.Incarnation {
		return fmt.Errorf("%w: active-session incarnation was replaced", session.ErrUnavailable)
	}
	return nil
}

// appendEntryLocked persists one candidate child and publishes it only after synchronization.
func (s *Service) appendEntryLocked(ctx context.Context, entry session.Entry) (session.Entry, error) {
	if s.writeUnavailable {
		return session.Entry{}, agentrun.ErrPersistenceUnavailable
	}
	entryID, err := s.ids.NewID()
	if err != nil {
		return session.Entry{}, fmt.Errorf("create session entry ID: %w", err)
	}
	entry.ID = entryID
	entry.ParentID = s.active.Tree.ActiveLeafID()
	entry.CreatedAt = s.clock.Now()
	candidateTree := s.active.Tree.Clone()
	if err = candidateTree.Add(entry); err != nil {
		return session.Entry{}, fmt.Errorf("validate session tree entry: %w", err)
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
		return session.Entry{}, fmt.Errorf("%w: append session entry: %w", agentrun.ErrPersistenceUnavailable, err)
	}
	// Publish active ownership only after the repository append is synchronized.
	s.active.StoragePath = result.StoragePath
	s.active.Tree = candidateTree
	return entry.Clone(), nil
}

// estimatedCost calculates one persisted request cost from disjoint normalized token buckets.
func (s *Service) estimatedCost(response model.Response) mo.Option[session.EstimatedCost] {
	usage, usagePresent := response.Usage.Get()
	providerID, providerPresent := response.Provider.Get()
	modelID, modelPresent := response.Model.Get()
	if !usagePresent || !providerPresent || !modelPresent {
		return mo.None[session.EstimatedCost]()
	}
	return s.estimatedUsageCost(providerID, modelID, mo.Some(session.TokenUsage{
		InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
		CacheReadTokens: usage.CachedInputTokens, CacheWriteTokens: usage.CacheWriteTokens,
		ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
	}))
}

// estimatedUsageCost calculates cost only when normalized usage and configured pricing are available.
func (s *Service) estimatedUsageCost(
	providerID model.ProviderID,
	modelID model.ID,
	usageOption mo.Option[session.TokenUsage],
) mo.Option[session.EstimatedCost] {
	usage, usagePresent := usageOption.Get()
	if !usagePresent {
		return mo.None[session.EstimatedCost]()
	}
	pricing, pricingPresent := s.pricing.Pricing(providerID, modelID).Get()
	if !pricingPresent {
		return mo.None[session.EstimatedCost]()
	}
	rates := model.PricingTier{
		InputTokensAbove: 0,
		Input:            pricing.Input,
		Output:           pricing.Output,
		CacheRead:        pricing.CacheRead,
		CacheWrite:       pricing.CacheWrite,
	}
	requestInput := usage.InputTokens + usage.CacheReadTokens + usage.CacheWriteTokens
	for tierIndex := range pricing.Tiers {
		if requestInput > pricing.Tiers[tierIndex].InputTokensAbove {
			rates = pricing.Tiers[tierIndex]
		}
	}
	const tokensPerMillion = 1_000_000
	cost := session.EstimatedCost{
		Input:      float64(usage.InputTokens) * rates.Input / tokensPerMillion,
		Output:     float64(usage.OutputTokens) * rates.Output / tokensPerMillion,
		CacheRead:  float64(usage.CacheReadTokens) * rates.CacheRead / tokensPerMillion,
		CacheWrite: float64(usage.CacheWriteTokens) * rates.CacheWrite / tokensPerMillion,
		Total:      0,
	}
	// Output already contains the reasoning subset, so no separate reasoning charge is added.
	cost.Total = cost.Input + cost.Output + cost.CacheRead + cost.CacheWrite
	return mo.Some(cost)
}
