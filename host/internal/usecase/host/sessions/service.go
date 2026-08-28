// Package sessions owns the active session and its persisted lifecycle information.
package sessions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
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

// sessionEntryCounts owns shared public-message counts for durable session entries.
type sessionEntryCounts struct {
	userMessages   int
	modelResponses int
	toolResults    int
	totalMessages  int
}

// add owns which durable entry kinds count as public messages.
func (counts *sessionEntryCounts) add(entry session.Entry) {
	if entry.User.IsSome() {
		counts.userMessages++
		counts.totalMessages++
	}
	if entry.Model.IsSome() {
		counts.modelResponses++
		counts.totalMessages++
	}
	if entry.ToolResult.IsSome() {
		counts.toolResults++
		counts.totalMessages++
	}
}

// countSessionEntries applies the shared public-message count rule to stored entries.
func countSessionEntries(entries []session.Entry) sessionEntryCounts {
	counts := sessionEntryCounts{
		userMessages: 0, modelResponses: 0, toolResults: 0, totalMessages: 0,
	}
	for entryIndex := range entries {
		counts.add(entries[entryIndex])
	}
	return counts
}

type providerModelKey struct {
	provider model.ProviderID
	model    model.ID
}

type accumulatedCost struct {
	value     session.EstimatedCost
	available bool
}

// statisticsFromEntries owns tool-call totals and complete token and cost availability.
func statisticsFromEntries(entries []session.Entry) session.Statistics {
	counts := countSessionEntries(entries)
	statistics := session.Statistics{
		UserMessages: counts.userMessages, ModelResponses: counts.modelResponses,
		ToolCalls: 0, ToolResults: counts.toolResults, TotalMessages: counts.totalMessages,
		TokenUsage: mo.Some(session.TokenUsage{}), EstimatedCost: mo.Some(session.EstimatedCost{}),
		CostBreakdown: nil,
	}
	usage := session.TokenUsage{}
	aggregateCost := accumulatedCost{value: session.EstimatedCost{}, available: true}
	groupCosts := make(map[providerModelKey]accumulatedCost)
	for entryIndex := range entries {
		entry := &entries[entryIndex]
		if response, present := entry.Model.Get(); present {
			for contentIndex := range response.Content {
				content := &response.Content[contentIndex]
				if content.Kind == model.ContentToolCall && content.ToolCall.IsSome() {
					statistics.ToolCalls++
				}
			}
			modelUsage, usagePresent := response.Usage.Get()
			if !usagePresent {
				// One absent model usage makes only token totals unavailable. Counts remain complete.
				statistics.TokenUsage = mo.None[session.TokenUsage]()
			} else {
				usage.InputTokens += modelUsage.InputTokens
				usage.OutputTokens += modelUsage.OutputTokens
				usage.CacheReadTokens += modelUsage.CachedInputTokens
				usage.CacheWriteTokens += modelUsage.CacheWriteTokens
				usage.ReasoningTokens += modelUsage.ReasoningTokens
				usage.TotalTokens += modelUsage.TotalTokens
			}
			accumulateEntryCost(entry, response, &aggregateCost, groupCosts)
		}
	}
	if statistics.TokenUsage.IsSome() {
		statistics.TokenUsage = mo.Some(usage)
	}
	if aggregateCost.available {
		statistics.EstimatedCost = mo.Some(aggregateCost.value)
	} else {
		statistics.EstimatedCost = mo.None[session.EstimatedCost]()
	}
	statistics.CostBreakdown = costBreakdown(groupCosts)
	return statistics
}

// accumulateEntryCost keeps aggregate and exact provider-model availability independent.
func accumulateEntryCost(
	entry *session.Entry,
	response model.Response,
	aggregate *accumulatedCost,
	groups map[providerModelKey]accumulatedCost,
) {
	cost, costPresent := entry.EstimatedCost.Get()
	if !costPresent {
		aggregate.available = false
	} else if aggregate.available {
		aggregate.value = addEstimatedCost(aggregate.value, cost)
	}
	providerID, providerPresent := response.Provider.Get()
	modelID, modelPresent := response.Model.Get()
	if !providerPresent || !modelPresent {
		return
	}
	key := providerModelKey{provider: providerID, model: modelID}
	group, found := groups[key]
	if !found {
		group = accumulatedCost{value: session.EstimatedCost{}, available: true}
	}
	if !costPresent {
		group.available = false
	} else if group.available {
		group.value = addEstimatedCost(group.value, cost)
	}
	groups[key] = group
}

// costBreakdown maps sorted exact provider-model keys into public availability values.
func costBreakdown(groups map[providerModelKey]accumulatedCost) []session.ProviderModelCost {
	if len(groups) == 0 {
		return nil
	}
	breakdown := make([]session.ProviderModelCost, 0, len(groups))
	for key, group := range groups {
		cost := mo.None[session.EstimatedCost]()
		if group.available {
			cost = mo.Some(group.value)
		}
		breakdown = append(breakdown, session.ProviderModelCost{
			Provider: key.provider, Model: key.model, EstimatedCost: cost,
		})
	}
	sort.Slice(breakdown, func(left, right int) bool {
		if breakdown[left].Provider != breakdown[right].Provider {
			return breakdown[left].Provider < breakdown[right].Provider
		}
		return breakdown[left].Model < breakdown[right].Model
	})
	return breakdown
}

// addEstimatedCost sums the five persisted cost values without recalculation.
func addEstimatedCost(left, right session.EstimatedCost) session.EstimatedCost {
	return session.EstimatedCost{
		Input: left.Input + right.Input, Output: left.Output + right.Output,
		CacheRead: left.CacheRead + right.CacheRead, CacheWrite: left.CacheWrite + right.CacheWrite,
		Total: left.Total + right.Total,
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

func terminalContinuationEntry(history agent.HistoryEntry) (session.Entry, bool, error) {
	entry := session.Entry{
		ID: "", CreatedAt: time.Time{}, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](),
	}
	switch history.Kind {
	case agent.HistoryEntryUser:
		entry.User = mo.Some(cloneMessage(history.User.MustGet()))
	case agent.HistoryEntryModel:
		response := history.Model.MustGet()
		outcome, terminal := response.Outcome.Get()
		if !terminal || outcome < model.OutcomeStop || outcome > model.OutcomeFailed {
			return session.Entry{}, false, nil
		}
		entry.Model = mo.Some(cloneModelResponse(response))
	case agent.HistoryEntryToolResult:
		entry.ToolResult = mo.Some(cloneToolResult(history.ToolResult.MustGet()))
	default:
		return session.Entry{}, false, fmt.Errorf("unsupported history entry kind %d", history.Kind)
	}
	return entry, true, nil
}

func historyFromEntries(entries []session.Entry) []agent.HistoryEntry {
	history := make([]agent.HistoryEntry, 0, len(entries))
	for entryIndex := range entries {
		entry := &entries[entryIndex]
		if user, present := entry.User.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryUser, User: mo.Some(cloneMessage(user)),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if response, present := entry.Model.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(cloneModelResponse(response)), ToolResult: mo.None[agent.ToolResult](),
			})
		}
		if result, present := entry.ToolResult.Get(); present {
			history = append(history, agent.HistoryEntry{
				Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](),
				Model: mo.None[model.Response](), ToolResult: mo.Some(cloneToolResult(result)),
			})
		}
	}
	return history
}

func cloneHistory(history []agent.HistoryEntry) []agent.HistoryEntry {
	cloned := slices.Clone(history)
	for index := range cloned {
		cloned[index], _ = cloneValidatedHistoryEntry(cloned[index])
	}
	return cloned
}

func cloneValidatedHistoryEntry(entry agent.HistoryEntry) (agent.HistoryEntry, error) {
	switch entry.Kind {
	case agent.HistoryEntryUser:
		message, present := entry.User.Get()
		if !present {
			return agent.HistoryEntry{}, errors.New("user history payload is missing")
		}
		return agent.HistoryEntry{
			Kind: entry.Kind, User: mo.Some(cloneMessage(message)), Model: mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
		}, nil
	case agent.HistoryEntryModel:
		response, present := entry.Model.Get()
		if !present {
			return agent.HistoryEntry{}, errors.New("model history payload is missing")
		}
		return agent.HistoryEntry{
			Kind: entry.Kind, User: mo.None[model.Message](), Model: mo.Some(cloneModelResponse(response)),
			ToolResult: mo.None[agent.ToolResult](),
		}, nil
	case agent.HistoryEntryToolResult:
		result, present := entry.ToolResult.Get()
		if !present {
			return agent.HistoryEntry{}, errors.New("tool-result history payload is missing")
		}
		return agent.HistoryEntry{
			Kind: entry.Kind, User: mo.None[model.Message](), Model: mo.None[model.Response](),
			ToolResult: mo.Some(cloneToolResult(result)),
		}, nil
	default:
		return agent.HistoryEntry{}, fmt.Errorf("unsupported history entry kind %d", entry.Kind)
	}
}

func cloneMessage(message model.Message) model.Message {
	message.Content = slices.Clone(message.Content)
	for index := range message.Content {
		message.Content[index].Data = message.Content[index].Data.MapValue(bytes.Clone)
	}
	return message
}

func cloneModelResponse(response model.Response) model.Response {
	content := slices.Clone(response.Content)
	for index := range content {
		cloneContext := func(value model.ProviderContext) model.ProviderContext {
			value.Payload = bytes.Clone(value.Payload)
			return value
		}
		content[index].ProviderContext = content[index].ProviderContext.MapValue(cloneContext)
		content[index].ToolCall = content[index].ToolCall.MapValue(func(value model.ToolCall) model.ToolCall {
			value.Arguments = cloneArguments(value.Arguments)
			return value
		})
	}
	return model.Response{
		Content: content, Outcome: response.Outcome, ErrorMessage: response.ErrorMessage,
		Provider: response.Provider, Model: response.Model, ResponseModel: response.ResponseModel,
		ResponseID: response.ResponseID, Usage: response.Usage, Diagnostics: slices.Clone(response.Diagnostics),
	}
}

func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := maps.Clone(arguments)
	for name, value := range cloned {
		cloned[name] = cloneJSONValue(value)
	}
	return cloned
}

func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		cloned := slices.Clone(typed)
		for index := range cloned {
			cloned[index] = cloneJSONValue(cloned[index])
		}
		return cloned
	default:
		return value
	}
}

func cloneToolResult(result agent.ToolResult) agent.ToolResult {
	contents := slices.Clone(result.Contents)
	for index := range contents {
		if image, present := contents[index].Image.Get(); present {
			image.Data = bytes.Clone(image.Data)
			contents[index].Image = mo.Some(image)
		}
	}
	return agent.ToolResult{
		CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError,
	}
}

func publicUserText(message model.Message) string {
	var text strings.Builder
	for _, content := range message.Content {
		if content.Kind == model.InputContentText {
			if value, present := content.Text.Get(); present {
				text.WriteString(value)
			}
		}
	}
	return text.String()
}

// replacementFromLoaded returns independent public state for an active-session replacement.
func replacementFromLoaded(loaded LoadedSession) session.Replacement {
	return session.Replacement{Info: infoFromLoaded(loaded), Entries: cloneEntries(loaded.Entries)}
}

// infoFromLoaded derives name and update time from ordered records rather than filesystem metadata.
func infoFromLoaded(loaded LoadedSession) session.Info {
	name := mo.None[string]()
	updatedAt := loaded.Header.CreatedAt
	for index := range loaded.Entries {
		entry := &loaded.Entries[index]
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

// cloneLoaded prevents repository-owned entries from becoming mutable active state.
func cloneLoaded(value LoadedSession) LoadedSession {
	return LoadedSession{
		Header: value.Header, StoragePath: value.StoragePath, Entries: cloneEntries(value.Entries),
	}
}

func cloneEntries(entries []session.Entry) []session.Entry {
	cloned := make([]session.Entry, len(entries))
	for index := range entries {
		entry := &entries[index]
		cloned[index] = session.Entry{
			ID: entry.ID, CreatedAt: entry.CreatedAt, Information: entry.Information,
			User: entry.User.MapValue(cloneMessage), Model: entry.Model.MapValue(cloneModelResponse),
			ToolResult: entry.ToolResult.MapValue(cloneToolResult),
			// Each active-session snapshot owns its opaque extension bytes.
			Extension: entry.Extension.MapValue(func(value session.ExtensionEnvelope) session.ExtensionEnvelope {
				value.Data = bytes.Clone(value.Data)
				return value
			}), EstimatedCost: entry.EstimatedCost,
		}
	}
	return cloned
}
