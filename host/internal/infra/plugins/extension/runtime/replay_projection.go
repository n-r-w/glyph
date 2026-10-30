package runtime

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// replayFragment retains private replay with the content kind that owned it.
type replayFragment struct {
	// kind identifies the only content kind that may receive the replay.
	kind model.ContentKind
	// replay contains the private provider context.
	replay model.ProviderContext
}

// replayProjection retains private replay of one history invocation behind invocation-local references.
type replayProjection struct {
	// fragments maps non-secret local references to their private replay.
	fragments map[string]replayFragment
	// nonce prevents references from another invocation being accepted accidentally.
	nonce string
}

// newReplayProjection creates an empty private replay table for one invocation.
func newReplayProjection() replayProjection {
	return replayProjection{fragments: make(map[string]replayFragment), nonce: rand.Text()}
}

// project maps history to its public representation with references in place of private replay.
func (projection replayProjection) project(history []agent.HistoryEntry) (*extensionpb.ModelContextHistory, error) {
	entries := make([]*extensionpb.ModelContextEntry, 0, len(history))
	for index := range history {
		entry := &history[index]
		mapped := new(extensionpb.ModelContextEntry)
		switch entry.Kind {
		case agent.HistoryEntryUser:
			mapped.SetUser(mapUserMessage(entry.User.OrEmpty()))
		case agent.HistoryEntryModel:
			response, err := projection.projectResponse(entry.Model.OrEmpty())
			if err != nil {
				return nil, fmt.Errorf("history entry %d: %w", index, err)
			}
			mapped.SetModel(response)
		case agent.HistoryEntryToolResult:
			mapped.SetToolResult(mapTreeToolResult(entry.ToolResult.OrEmpty()))
		default:
			return nil, fmt.Errorf("history entry %d has unknown kind %d", index, entry.Kind)
		}
		entries = append(entries, mapped)
	}
	return extensionpb.ModelContextHistory_builder{Entries: entries}.Build(), nil
}

// projectResponse maps model-visible content and records private replay of each block.
func (projection replayProjection) projectResponse(
	response model.Response,
) (*extensionpb.ReplayableModelResponse, error) {
	visible, err := extensioncontroller.MapSessionTreeModelResponse(response)
	if err != nil {
		return nil, err
	}
	contents := make([]*extensionpb.ReplayableModelContent, len(visible.GetContent()))
	for index, content := range visible.GetContent() {
		builder := extensionpb.ReplayableModelContent_builder{Content: content, ReplayReference: nil}
		source := &response.Content[index]
		if replay, present := source.ProviderContext.Get(); present {
			builder.ReplayReference = new(projection.reference(source.Kind, replay))
		}
		contents[index] = builder.Build()
	}
	return extensionpb.ReplayableModelResponse_builder{Content: contents}.Build(), nil
}

// reference records private replay and returns its non-secret local reference.
func (projection replayProjection) reference(kind model.ContentKind, replay model.ProviderContext) string {
	id := projection.nonce + "-" + strconv.Itoa(len(projection.fragments))
	projection.fragments[id] = replayFragment{kind: kind, replay: replay}
	return id
}

// restore decodes replacement history and restores each retained reference at most once.
func (projection replayProjection) restore(history *extensionpb.ModelContextHistory) ([]agent.HistoryEntry, error) {
	used := make(map[string]bool)
	entries := make([]agent.HistoryEntry, len(history.GetEntries()))
	for index, value := range history.GetEntries() {
		entry, err := projection.restoreEntry(value, used)
		if err != nil {
			return nil, fmt.Errorf("history entry %d: %w", index, err)
		}
		entries[index] = entry
	}
	return entries, nil
}

// restoreEntry decodes one replacement entry.
func (projection replayProjection) restoreEntry(
	value *extensionpb.ModelContextEntry,
	used map[string]bool,
) (agent.HistoryEntry, error) {
	entry := agent.HistoryEntry{
		Kind:       0,
		User:       mo.None[model.Message](),
		Model:      mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}
	switch value.WhichContent() {
	case extensionpb.ModelContextEntry_User_case:
		message, err := extensioncontroller.DecodeSessionTreeUser(value.GetUser())
		if err != nil {
			return agent.HistoryEntry{}, err
		}
		entry.Kind = agent.HistoryEntryUser
		entry.User = mo.Some(message)
	case extensionpb.ModelContextEntry_Model_case:
		response, err := projection.restoreResponse(value.GetModel(), used)
		if err != nil {
			return agent.HistoryEntry{}, err
		}
		entry.Kind = agent.HistoryEntryModel
		entry.Model = mo.Some(response)
	case extensionpb.ModelContextEntry_ToolResult_case:
		result, err := extensioncontroller.DecodeSessionTreeToolResult(value.GetToolResult())
		if err != nil {
			return agent.HistoryEntry{}, err
		}
		entry.Kind = agent.HistoryEntryToolResult
		entry.ToolResult = mo.Some(result)
	case extensionpb.ModelContextEntry_Content_not_set_case:
		return agent.HistoryEntry{}, errors.New("history entry content is missing")
	}
	return entry, nil
}

// restoreResponse decodes model-visible content and restores replay at its owning content kind.
func (projection replayProjection) restoreResponse(
	value *extensionpb.ReplayableModelResponse,
	used map[string]bool,
) (model.Response, error) {
	visible := make([]*extensionpb.SessionTreeModelContent, len(value.GetContent()))
	for index, content := range value.GetContent() {
		visible[index] = content.GetContent()
	}
	contents, err := extensioncontroller.DecodeSessionTreeModelContents(visible)
	if err != nil {
		return model.Response{}, err
	}
	for index, content := range value.GetContent() {
		reference := content.GetReplayReference()
		if reference == "" {
			continue
		}
		fragment, known := projection.fragments[reference]
		switch {
		case !known:
			return model.Response{}, fmt.Errorf("content %d has unknown private replay reference %q", index, reference)
		case used[reference]:
			return model.Response{}, fmt.Errorf("content %d duplicates private replay reference %q", index, reference)
		case fragment.kind != contents[index].Kind:
			return model.Response{}, fmt.Errorf("content %d misplaces private replay reference %q", index, reference)
		}
		used[reference] = true
		contents[index].ProviderContext = mo.Some(fragment.replay)
	}
	return model.Response{
		Content: contents, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}, nil
}
