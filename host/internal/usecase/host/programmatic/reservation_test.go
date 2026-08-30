package programmatic

import (
	"sync"
	"sync/atomic"

	"github.com/samber/mo"

	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestConcurrentReservationRejectsOneRequest verifies active reservation is atomic.
func (s *ServiceSuite) TestConcurrentReservationRejectsOneRequest() {
	// Arrange two run preparations that cross the reservation boundary together.
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	var prepareBarrier sync.WaitGroup
	prepareBarrier.Add(2)
	var runNumber atomic.Int64
	coordinator.EXPECT().PrepareRun().DoAndReturn(func() (string, error) {
		prepareBarrier.Done()
		prepareBarrier.Wait()
		if runNumber.Add(1) == 1 {
			return "run-1", nil
		}
		return "run-2", nil
	}).Times(2)

	// Act by submitting both requests concurrently.
	type result struct {
		response  controller.Response
		operation controller.Operation
		err       error
	}
	results := make([]result, 2)
	var calls sync.WaitGroup
	calls.Add(2)
	for index := range results {
		go func() {
			defer calls.Done()
			results[index].response, results[index].operation, results[index].err = service.Handle(
				s.T().Context(), controller.Command{
					CorrelationID: string(rune('a' + index)), Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
					SessionID:   mo.None[session.ID](),
					SessionName: mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
				},
			)
		}()
	}
	calls.Wait()

	// Assert exactly one request owns the reservation and the other is rejected as busy.
	accepted := 0
	rejected := 0
	for _, result := range results {
		s.Require().NoError(result.err)
		switch result.response.Kind {
		case controller.ResponseUserRequestAccepted:
			accepted++
			s.Require().NotNil(result.operation)
		case controller.ResponseRejected:
			rejected++
			s.Equal(controller.RejectionBusy, result.response.Rejection.OrEmpty().Code)
		case controller.ResponseUnspecified,
			controller.ResponseAbortCompleted,
			controller.ResponseRunState,
			controller.ResponseMessages,
			controller.ResponseModels,
			controller.ResponseModelSelection,
			controller.ResponseSessionInfo,
			controller.ResponseSessions,
			controller.ResponseSessionEntries,
			controller.ResponseSessionStats,
			controller.ResponseSessionTree,
			controller.ResponseSessionTreeNavigation,
			controller.ResponseForkSession,
			controller.ResponseCloneSession,
			controller.ResponseSetEntryLabel:
			s.Fail("unexpected response", "kind %d", result.response.Kind)
		}
	}
	s.Equal(1, accepted)
	s.Equal(1, rejected)
	s.Require().NoError(service.CancelAndWait(s.T().Context()))
}

// TestDisconnectPreventsLateReservation verifies in-flight acceptance cannot outlive session cleanup.
func (s *ServiceSuite) TestDisconnectPreventsLateReservation() {
	// Arrange a run preparation that completes only after disconnect begins.
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	service := New(coordinator, nil, idleStateSnapshot, emptyHistorySnapshot, nil, NewDelivery())
	preparing := make(chan struct{})
	prepared := make(chan struct{})
	coordinator.EXPECT().PrepareRun().DoAndReturn(func() (string, error) {
		close(preparing)
		<-prepared
		return "run-late", nil
	})
	type handleResult struct {
		response  controller.Response
		operation controller.Operation
		err       error
	}
	// Act by starting the request, disconnecting, and then releasing preparation.
	result := make(chan handleResult)
	go func() {
		response, operation, err := service.Handle(s.T().Context(), controller.Command{
			CorrelationID: "late", Kind: controller.CommandUserRequest, UserText: mo.Some("request"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
			SessionID:   mo.None[session.ID](),
			SessionName: mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
		})
		result <- handleResult{response: response, operation: operation, err: err}
	}()
	<-preparing

	s.Require().NoError(service.CancelAndWait(s.T().Context()))
	close(prepared)
	handled := <-result

	// Assert the late prepared run is rejected and never reserved.
	s.Require().NoError(handled.err)
	s.Nil(handled.operation)
	s.Equal(controller.ResponseRejected, handled.response.Kind)
	s.Equal(controller.RejectionBusy, handled.response.Rejection.OrEmpty().Code)
}

// TestQueriesReturnPublicSnapshotsDuringAcceptedRun verifies live queries expose complete owned public history.
func (s *ServiceSuite) TestQueriesReturnPublicSnapshotsDuringAcceptedRun() {
	// Arrange a running service with full user, model, diagnostic, and tool-result history.
	ctrl := gomock.NewController(s.T())
	coordinator := NewMockCoordinator(ctrl)
	coordinator.EXPECT().CancelPrepared(gomock.Any()).AnyTimes()
	delivery := NewDelivery()
	state := run.State{
		Status: run.StatusRunning, RunID: mo.Some("run-active"),
		PartialResponse: mo.Some(model.Response{Content: []model.Content{{Kind: model.ContentText, Text: mo.Some("partial"), Final: false, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}}, Outcome: mo.None[model.Outcome](), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil}),
		ToolPreviews:    map[string]model.ToolCallPreview{"preview": {CallID: "preview", Name: "", Position: 0, Provisional: false, Fields: nil}},
	}
	var history []agent.HistoryEntry
	service := New(coordinator, nil, func() run.State { return state }, func() []agent.HistoryEntry { return history }, nil, delivery)
	coordinator.EXPECT().PrepareRun().Return("run-active", nil)

	// Act by accepting a run and querying state and messages while it remains active.
	_, operation, err := service.Handle(s.T().Context(), controller.Command{
		CorrelationID: "active", Kind: controller.CommandUserRequest, UserText: mo.Some("first"), ProviderID: mo.None[model.ProviderID](), ModelID: mo.None[model.ID](), ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:   mo.None[session.ID](),
		SessionName: mo.None[string](), TargetEntryID: mo.None[string](), SummaryMode: controller.SummaryModeNoSummary, CustomFocus: mo.None[string](), EntryLabel: mo.None[string](),
	})
	s.Require().NoError(err)
	s.Require().NotNil(operation)
	defer func() { s.Require().NoError(service.CancelAndWait(s.T().Context())) }()

	response, returnedOperation, err := service.Handle(s.T().Context(), testProgrammaticCommand("state", controller.CommandGetRunState))
	s.Require().NoError(err)
	s.Nil(returnedOperation)
	s.Equal(controller.Response{
		SessionEntries: nil,
		CorrelationID:  "state", Kind: controller.ResponseRunState,
		State: mo.Some(controller.RunStateResult{State: controller.RunStateRunning, ActiveCorrelationID: mo.Some("active")}), Messages: nil, Models: mo.None[controller.ModelsResult](), Selection: mo.None[model.Selection](), Rejection: mo.None[controller.Rejection](),
		SessionInfo:       mo.None[session.Info](),
		Sessions:          nil,
		SessionStatistics: mo.None[session.Statistics](), SessionTree: mo.None[controller.SessionTree](), TreeNavigation: mo.None[controller.TreeNavigationResult](), Replacement: mo.None[controller.SessionReplacement](),
	}, response)

	responseModel := model.ID("response-model")
	history = []agent.HistoryEntry{
		{Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("hello")), Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult]()},
		{Kind: agent.HistoryEntryModel, Model: mo.Some(model.Response{
			Content: []model.Content{
				{Kind: model.ContentText, Text: mo.Some("answer"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentText, Text: mo.Some("partial"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentReasoning, ProviderContext: mo.Some(model.ProviderContext{Source: model.ProviderContextSource{ProviderID: "provider", API: "", Model: "", CompatibilityKey: mo.None[string]()}, Payload: []byte(`{"secret":true}`)}), Text: mo.None[string](), Final: true, ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentReasoning, Text: mo.Some("reason"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
				{Kind: model.ContentRefusal, Text: mo.Some("refusal"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()},
			},
			Outcome: mo.Some(model.OutcomeStop), Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")), ResponseModel: mo.Some(responseModel), ErrorMessage: mo.None[string](), ResponseID: mo.Some("response-id"),
			Usage: mo.Some(model.Usage{
				InputTokens: 3, OutputTokens: 2, CachedInputTokens: 1,
				CacheWriteTokens: 0, ReasoningTokens: 1, TotalTokens: 5,
			}),
			Diagnostics: []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}},
		}), User: mo.None[model.Message](), ToolResult: mo.None[agent.ToolResult](),
		},
		{Kind: agent.HistoryEntryToolResult, ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "tool",
			Contents: []tool.ResultContent{
				{Kind: tool.ResultContentText, Text: mo.Some("output"), Image: mo.None[tool.ResultImage]()},
				{Kind: tool.ResultContentImage, Text: mo.None[string](), Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{1, 2}})},
			}, IsError: false,
		}), User: mo.None[model.Message](), Model: mo.None[model.Response](),
		},
	}
	response, returnedOperation, err = service.Handle(s.T().Context(), testProgrammaticCommand("messages", controller.CommandGetMessages))
	// Assert the query includes ordered public content without private provider context.
	s.Require().NoError(err)
	s.Nil(returnedOperation)
	s.Equal(controller.ResponseMessages, response.Kind)
	s.Require().Len(response.Messages, 3)
	s.Equal(model.TextMessage("hello"), response.Messages[0].User.MustGet())
	modelResponse := response.Messages[1].Model.OrEmpty()
	s.Equal("answerpartialrefusal", modelResponse.Text)
	s.Equal(mo.Some(controller.ModelOutcomeStop), modelResponse.Outcome)
	s.Equal(mo.Some("provider"), modelResponse.Provider)
	s.Equal(mo.Some("model"), modelResponse.Model)
	s.Equal(mo.Some("response-model"), modelResponse.ResponseModel)
	s.Equal(mo.Some("response-id"), modelResponse.ResponseID)
	s.Equal(mo.Some(controller.ModelUsage{
		InputTokens: 3, OutputTokens: 2, CachedInputTokens: 1,
		CacheWriteTokens: 0, ReasoningTokens: 1, TotalTokens: 5,
	}), modelResponse.Usage)
	s.Equal([]controller.ModelDiagnostic{{Code: "notice", Message: "safe diagnostic"}}, modelResponse.Diagnostics)
	s.Require().Len(modelResponse.Content, 4)
	s.Equal(controller.ModelResponseContentText, modelResponse.Content[0].Kind)
	s.Equal(controller.ModelResponseContentText, modelResponse.Content[1].Kind)
	s.Equal(controller.ModelResponseContentReasoning, modelResponse.Content[2].Kind)
	s.Equal(controller.ModelResponseContentRefusal, modelResponse.Content[3].Kind)
	publicToolResult := response.Messages[2].ToolResult.OrEmpty()
	s.Equal("call", publicToolResult.CallID)
	s.Require().Len(publicToolResult.Contents, 2)
	s.Equal("output", publicToolResult.Contents[0].Text.OrEmpty())
	s.Equal([]byte{1, 2}, publicToolResult.Contents[1].Image.MustGet().Data)
}
