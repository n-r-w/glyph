package contextcompaction

import (
	"context"
	"errors"
	"reflect"
	"sync"

	"github.com/samber/mo"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// defaultRetainedContextBudget is the Host fallback for recent context retained after compaction.
const defaultRetainedContextBudget int64 = 20_000

// reportedUsageBaseline keeps one detached completed conversation request and response.
type reportedUsageBaseline struct {
	// identity identifies the active session incarnation that produced the response.
	identity session.Identity
	// request contains the exact provider-neutral request snapshot.
	request modelexecution.ProviderRequest
	// response contains the exact delivered terminal response.
	response model.Response
	// usage contains normalized reported usage, including a present zero value.
	usage model.Usage
}

// Service owns active-conversation sizing and its reported-usage baseline.
type Service struct {
	// mutex protects the process-local reported-usage baseline.
	mutex sync.Mutex
	// sessions supplies active-session identity and compaction persistence.
	sessions SessionState
	// runtime snapshots and invokes public compaction capabilities.
	runtime Runtime
	// contexts supplies session-bound contexts for compaction invocations.
	contexts ContextIssuer
	// retainedBudget is the configured recent-context target.
	retainedBudget int64
	// manualModel supplies the active model snapshot for manual operations.
	manualModel ManualModel
	// manualTools supplies the active tool catalog for manual operations.
	manualTools ManualTools
	// manualInstructions contains the resolved agent instructions used by manual operations.
	manualInstructions string
	// baseline contains at most one completed conversation usage observation.
	baseline mo.Option[reportedUsageBaseline]
}

var (
	_ controllerextension.CompactionOperations = (*Service)(nil)
	_ modelexecution.ConversationContext       = (*Service)(nil)
	_ modelexecution.ContextPreparation        = (*Service)(nil)
	_ programmatic.Compactor                   = (*Service)(nil)
	_ startup.CompactionRegistrar              = (*Service)(nil)
	_ ui.Compactor                             = (*Service)(nil)
)

// New creates the context-compaction owner.
func New(sessions SessionState) *Service {
	return &Service{
		mutex:              sync.Mutex{},
		sessions:           sessions,
		runtime:            nil,
		contexts:           nil,
		retainedBudget:     defaultRetainedContextBudget,
		manualModel:        nil,
		manualTools:        nil,
		manualInstructions: "",
		baseline:           mo.None[reportedUsageBaseline](),
	}
}

// BindOrchestration connects extension invocation and the configured retained-context target.
func (s *Service) BindOrchestration(runtime Runtime, contexts ContextIssuer, retainedBudget int64) error {
	if s.runtime != nil || s.contexts != nil {
		return errors.New("compaction orchestration is already bound")
	}
	if runtime == nil || contexts == nil {
		return errors.New("compaction runtime and context issuer are required")
	}
	if retainedBudget < 0 {
		return errors.New("compaction retained-context budget must be nonnegative")
	}
	s.runtime = runtime
	s.contexts = contexts
	s.retainedBudget = retainedBudget
	return nil
}

// BindManualRequest connects active model, tools, and resolved instructions for manual operations.
func (s *Service) BindManualRequest(modelSource ManualModel, tools ManualTools, instructions string) error {
	if s.manualModel != nil || s.manualTools != nil {
		return errors.New("manual compaction request sources are already bound")
	}
	if modelSource == nil || tools == nil {
		return errors.New("manual compaction model and tools are required")
	}
	s.manualModel = modelSource
	s.manualTools = tools
	s.manualInstructions = instructions
	return nil
}

// EstimateContext estimates the exact provider-neutral request projection.
func (s *Service) EstimateContext(request modelexecution.ProviderRequest) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	baseline, present := s.baseline.Get()
	if !present || !baselineMatches(baseline, s.sessions.ContextSession(), request) {
		return fallbackEstimate(request)
	}
	prefixLength := len(baseline.request.History) + 1
	tailEstimate, err := estimateHistory(request.History[prefixLength:])
	if err != nil {
		return 0, err
	}
	return normalizedUsageTotal(baseline.usage) + tailEstimate, nil
}

// ObserveCompletedConversation records eligible reported usage for a later request estimate.
func (s *Service) ObserveCompletedConversation(request modelexecution.ProviderRequest, response model.Response) {
	outcome, outcomePresent := response.Outcome.Get()
	usage, usagePresent := response.Usage.Get()
	if !outcomePresent || !usagePresent || !validNormalizedUsage(usage) ||
		outcome != model.OutcomeStop && outcome != model.OutcomeToolUse && outcome != model.OutcomeLength {
		return
	}
	ownedRequest := cloneProviderRequest(request)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.baseline = mo.Some(reportedUsageBaseline{
		identity: s.sessions.ContextSession(), request: ownedRequest,
		response: response.Clone(), usage: usage,
	})
}

// CommitCompaction persists one validated marker through the session owner.
func (s *Service) CommitCompaction(
	ctx context.Context,
	expected session.Identity,
	expectedLeafID mo.Option[string],
	compaction session.CompactionEntry,
) (session.Entry, error) {
	committed, err := s.sessions.CommitCompaction(ctx, expected, expectedLeafID, compaction)
	if committed.ID != "" {
		s.InvalidateBaseline()
	}
	return committed, err
}

// InvalidateBaseline clears reported usage after a committed compaction.
func (s *Service) InvalidateBaseline() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.baseline = mo.None[reportedUsageBaseline]()
}

// baselineMatches checks every approved identity and exact-prefix reuse condition.
func baselineMatches(
	baseline reportedUsageBaseline,
	identity session.Identity,
	request modelexecution.ProviderRequest,
) bool {
	if baseline.identity.ID != identity.ID || baseline.identity.Incarnation != identity.Incarnation ||
		baseline.request.Instructions != request.Instructions ||
		baseline.request.ReasoningChoice != request.ReasoningChoice ||
		!reflect.DeepEqual(baseline.request.Model, request.Model) ||
		!reflect.DeepEqual(baseline.request.Tools, request.Tools) {
		return false
	}
	expectedLength := len(baseline.request.History) + 1
	if len(request.History) < expectedLength {
		return false
	}
	for index := range baseline.request.History {
		if !reflect.DeepEqual(baseline.request.History[index], request.History[index]) {
			return false
		}
	}
	return reflect.DeepEqual(request.History[len(baseline.request.History)], agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
		Model: mo.Some(baseline.response), ToolResult: mo.None[agent.ToolResult](),
	})
}

// normalizedUsageTotal returns the approved total without adding reasoning twice.
func normalizedUsageTotal(usage model.Usage) int64 {
	return usage.InputTokens + usage.CachedInputTokens + usage.CacheWriteTokens + usage.OutputTokens
}

// validNormalizedUsage checks the disjoint buckets supplied by the completed model execution.
func validNormalizedUsage(usage model.Usage) bool {
	return usage.InputTokens >= 0 && usage.OutputTokens >= 0 && usage.CachedInputTokens >= 0 &&
		usage.CacheWriteTokens >= 0 && usage.ReasoningTokens >= 0 && usage.ReasoningTokens <= usage.OutputTokens &&
		usage.TotalTokens == normalizedUsageTotal(usage)
}

// cloneProviderRequest detaches every mutable request value retained by the baseline.
func cloneProviderRequest(request modelexecution.ProviderRequest) modelexecution.ProviderRequest {
	cloned := request
	cloned.Model = request.Model.Clone()
	if request.History != nil {
		cloned.History = make([]agent.HistoryEntry, len(request.History))
		for index := range request.History {
			cloned.History[index] = request.History[index].Clone()
		}
	}
	if request.Tools != nil {
		cloned.Tools = make([]tool.Descriptor, len(request.Tools))
		for index := range request.Tools {
			cloned.Tools[index] = request.Tools[index]
			cloned.Tools[index].InputSchemaJSON = append([]byte(nil), request.Tools[index].InputSchemaJSON...)
		}
	}
	return cloned
}
