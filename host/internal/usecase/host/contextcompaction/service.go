package contextcompaction

import (
	"context"
	"reflect"
	"sync"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// reportedUsageBaseline keeps one detached completed conversation request and response.
type reportedUsageBaseline struct {
	// identity identifies the active session incarnation that produced the response.
	identity SessionIdentity
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
	// baseline contains at most one completed conversation usage observation.
	baseline mo.Option[reportedUsageBaseline]
}

var _ modelexecution.ConversationContext = (*Service)(nil)

// New creates the context-compaction owner.
func New(sessions SessionState) *Service {
	return &Service{
		mutex: sync.Mutex{}, sessions: sessions, baseline: mo.None[reportedUsageBaseline](),
	}
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
	expected SessionIdentity,
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
	identity SessionIdentity,
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
