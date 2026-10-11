//go:build !integration

// Package extensionv1 supplies the public Extension SDK.
package extensionv1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// handlerMatrixSuite owns the current registration payload matrix.
type handlerMatrixSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestHandlerPayloadContracts checks every registered current kind and malformed payload.
func TestHandlerPayloadContracts(t *testing.T) {
	// Arrange parallel execution of the handler payload matrix.
	t.Parallel()
	// Act through matching and malformed handler payloads.
	// Assert the registration-kind contract for each payload in TestScenario.
	suite.Run(t, &handlerMatrixSuite{Suite: suite.Suite{}})
}

// TestScenario checks handler payload matching, missing fields, and unknown kinds.
func (s *handlerMatrixSuite) TestScenario() {
	t := s.T()
	const kindFailureFormat = "kind %d"
	// Arrange one public payload for every current handler kind.
	for _, scenario := range []struct {
		kind    extensionpb.HandlerKind
		request *extensionpb.HandleRequest
	}{
		{kind: extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetSessionBeforeTreeRequest(new(extensionpb.SessionBeforeTreeRequestInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetSessionBeforeTreeResult(new(extensionpb.SessionBeforeTreeResultInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetSessionTree(new(extensionpb.SessionTreeInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_MODEL_SELECTION, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetModelSelection(new(extensionpb.SelectionHandlerInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_REASONING_SELECTION, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetReasoningSelection(new(extensionpb.SelectionHandlerInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_RETRY, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetRetry(new(extensionpb.RetryHandlerInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_REQUEST, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetCompactionRequest(new(extensionpb.CompactionRequestInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_GENERATE, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetCompactionGenerate(new(extensionpb.CompactionRequestInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_RESULT, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetCompactionResult(new(extensionpb.CompactionResultInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_SUCCESS, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetCompactionSuccess(new(extensionpb.CompactionOutcome))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_FAILURE, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetCompactionFailure(new(extensionpb.CompactionOutcome))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_COMPACTION_POLICY, request: func() *extensionpb.HandleRequest {
			request := new(extensionpb.HandleRequest)
			request.SetCompactionPolicy(new(extensionpb.CompactionPolicyInvocation))
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_AGENT_START, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetAgentStart(new(extensionpb.AgentStart))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_AGENT_END, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetAgentEnd(new(extensionpb.AgentEnd))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_AGENT_SETTLED, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetAgentSettled(new(extensionpb.AgentSettled))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_TURN_START, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetTurnStart(new(extensionpb.TurnStart))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_TURN_END, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetTurnEnd(new(extensionpb.TurnEnd))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_MESSAGE_START, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetMessageStart(new(extensionpb.MessageStart))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_MESSAGE_UPDATE, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetMessageUpdate(new(extensionpb.MessageUpdate))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_MESSAGE_END, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetMessageEnd(new(extensionpb.MessageEnd))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_TOOL_EXECUTION_START, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetToolExecutionStart(new(extensionpb.ToolExecutionStart))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_TOOL_EXECUTION_UPDATE, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetToolExecutionUpdate(new(extensionpb.ToolExecutionUpdate))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_TOOL_EXECUTION_END, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetToolExecutionEnd(new(extensionpb.ToolExecutionEnd))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_MODEL_SELECTION_OBSERVER, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetModelSelection(new(extensionpb.ModelSelectionChanged))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
		{kind: extensionpb.HandlerKind_HANDLER_KIND_REASONING_SELECTION_OBSERVER, request: func() *extensionpb.HandleRequest {
			invocation := new(extensionpb.LifecycleInvocation)
			invocation.SetReasoningSelection(new(extensionpb.ReasoningSelectionChanged))
			request := new(extensionpb.HandleRequest)
			request.SetLifecycle(invocation)
			return request
		}()},
	} {
		// Act through the registration-kind contract.
		matched := handlerKindMatches(scenario.kind, scenario.request)
		// Assert preserved matching and absence behavior.
		require.True(t, matched, kindFailureFormat, scenario.kind)
		require.False(t, handlerKindMatches(scenario.kind, new(extensionpb.HandleRequest)))
	}
	require.False(
		t,
		handlerKindMatches(extensionpb.HandlerKind_HANDLER_KIND_UNSPECIFIED, new(extensionpb.HandleRequest)),
	)
	require.False(t, handlerKindMatches(extensionpb.HandlerKind(999), new(extensionpb.HandleRequest)))
}
