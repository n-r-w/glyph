//go:build !integration

package run

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
)

// TestRunStreamsEffectiveContext requests effective context and executes it through the returned provider.
func TestRunStreamsEffectiveContext(t *testing.T) {
	t.Parallel()
	// Arrange an effective context whose request differs from the assembled Core request.
	controller := gomock.NewController(t)
	contexts := NewMockEffectiveContextPort(controller)
	provider := NewMockModelProvider(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	descriptor := tool.Descriptor{
		Name: "read", Description: "read", InputSchemaJSON: []byte(`{}`),
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
	}
	tools.EXPECT().Tools().Return([]tool.Descriptor{descriptor})
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	user := agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("hi")), Model: mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}
	marker := agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("request-local")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
	effective := ModelRequest{
		Instructions: "effective instructions", Model: testModelDescriptor, ReasoningChoice: model.ReasoningChoiceHigh,
		History: []agent.HistoryEntry{user, marker}, Tools: []tool.Descriptor{descriptor},
	}
	contexts.EXPECT().PrepareContext(gomock.Any(), ContextRequest{
		Instructions: testInstructions, History: []agent.HistoryEntry{user}, Tools: []tool.Descriptor{descriptor},
	}).Return(EffectiveContext{Request: effective, Provider: provider}, nil)
	response := model.Response{
		Content: []model.Content{testTextItem("done")}, Outcome: mo.Some(model.OutcomeStop),
		ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](),
		Diagnostics: nil,
	}
	provider.EXPECT().Stream(gomock.Any(), effective, gomock.Any()).DoAndReturn(streamResult(response, nil))
	service := New(testInstructions, contexts, tools, events, newMockHistoryStore(t))

	// Act through one ordinary run.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", UserText: "hi"})

	// Assert completion and that the request-local entry did not enter persisted history.
	require.NoError(t, err)
	require.Equal(t, agent.RunOutcomeCompleted, result.Outcome)
	require.Len(t, service.History(), 2)
	require.Equal(t, user, service.History()[0])
	require.Equal(t, mo.Some(response), service.History()[1].Model)
}

// TestRunFailsWhenEffectiveContextFails ends the turn with the complete cause before any provider execution.
func TestRunFailsWhenEffectiveContextFails(t *testing.T) {
	t.Parallel()
	// Arrange a context failure and a provider that must not be used.
	controller := gomock.NewController(t)
	contexts := NewMockEffectiveContextPort(controller)
	tools := NewMockToolRuntime(controller)
	events := NewMockEventSink(controller)
	tools.EXPECT().Tools().Return(nil)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	source := errors.New("middleware handler \"context\": extension source text")
	contexts.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).Return(EffectiveContext{}, source)
	service := New(testInstructions, contexts, tools, events, newMockHistoryStore(t))

	// Act through one ordinary run.
	result, err := service.Run(t.Context(), runcontrol.Request{RunID: "run", UserText: "hi"})

	// Assert the failed outcome and the persisted failed response keep the complete cause.
	require.ErrorIs(t, err, source)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	require.Len(t, service.History(), 2)
	require.Contains(t, service.History()[1].Model.MustGet().ErrorMessage.OrEmpty(), source.Error())
}
