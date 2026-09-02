//go:build integration

package uiv1

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// malformedAgentEvent creates one progress event after applying its malformed fields.
func malformedAgentEvent(kind uiv1.LifecycleType, mutate func(*uiv1.AgentEvent)) *uiv1.HostEvent {
	event := new(uiv1.AgentEvent)
	event.SetType(kind)
	event.SetRunId("run")
	mutate(event)
	progress := new(uiv1.HostProgress)
	progress.SetAgentEvent(event)
	result := new(uiv1.HostEvent)
	result.SetProgress(progress)
	return result
}

// malformedCompletion creates one malformed completion event.
func malformedCompletion(set func(*uiv1.HostCompleted)) *uiv1.HostEvent {
	completed := new(uiv1.HostCompleted)
	set(completed)
	event := new(uiv1.HostEvent)
	event.SetCompleted(completed)
	return event
}

// validModelContent creates one otherwise valid model content payload.
func validModelContent(contentType uiv1.ModelContentType) *uiv1.ModelContent {
	content := new(uiv1.ModelContent)
	content.SetType(contentType)
	content.SetPosition(0)
	content.SetText("text")
	content.SetKind(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT)
	return content
}

// validToolCallPreview creates one preview with all required scalars.
func validToolCallPreview() *uiv1.ToolCallPreview {
	preview := new(uiv1.ToolCallPreview)
	preview.SetCallId("call")
	preview.SetName("tool")
	preview.SetPosition(0)
	preview.SetProvisional(true)
	return preview
}

// validSessionInfo creates one complete session identity.
func validSessionInfo() *uiv1.SessionInfo {
	info := new(uiv1.SessionInfo)
	info.SetId("session")
	info.SetWorkingDirectory("/workspace")
	info.SetCreatedTime(timestamppb.Now())
	info.SetUpdateTime(timestamppb.Now())
	return info
}

// acceptedHostEvent creates one Accepted lifecycle event.
func acceptedHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetAccepted(new(operationv1.Accepted))
	return event
}

// runningHostEvent creates one Running lifecycle event.
func runningHostEvent() *uiv1.HostEvent {
	event := new(uiv1.HostEvent)
	event.SetRunning(new(operationv1.Running))
	return event
}

// validCompletedHostEvent creates a matching completion for a progress test request.
func validCompletedHostEvent(request *uiv1.UIRequest) *uiv1.HostEvent {
	return malformedCompletion(func(value *uiv1.HostCompleted) {
		if request.GetRetryAuthentication() != nil {
			value.SetAuthentication(new(uiv1.AuthenticationCompleted))
			return
		}
		value.SetSubmit(new(uiv1.SubmitCompleted))
	})
}

// hostOperationEvent wraps one Host lifecycle event for the tracked operation.
func hostOperationEvent(event *uiv1.HostEvent) *uiv1.OpenRequest {
	return uiv1.OpenRequest_builder{
		OperationId: new("operation"), Request: nil, Event: event, ConnectionEvent: nil, Close: nil,
	}.Build()
}

// submitUIRequest creates one submit request.
func submitUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetSubmit(uiv1.SubmitCommand_builder{Text: new("request")}.Build())
	return request
}

// cancelUIRequest creates one cancellation request.
func cancelUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetCancel(operationv1.CancelOperation_builder{TargetOperationId: new("target")}.Build())
	return request
}

// authenticationUIRequest creates one authentication request.
func authenticationUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetRetryAuthentication(new(uiv1.RetryAuthenticationCommand))
	return request
}

// modelSelectionUIRequest creates one model selection request.
func modelSelectionUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetSelectModel(new(uiv1.SelectModelCommand))
	return request
}

// createSessionUIRequest creates one session creation request.
func createSessionUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetCreateSession(new(uiv1.CreateSessionCommand))
	return request
}

// listSessionsUIRequest creates one session list request.
func listSessionsUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetListSessions(new(uiv1.ListSessionsCommand))
	return request
}

// sessionInfoUIRequest creates one session information request.
func sessionInfoUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetGetSessionInfo(new(uiv1.GetSessionInfoCommand))
	return request
}

// sessionTreeUIRequest creates one session tree request.
func sessionTreeUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetGetSessionTree(new(uiv1.GetSessionTreeCommand))
	return request
}

// navigationUIRequest creates one session tree navigation request.
func navigationUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetNavigateSessionTree(new(uiv1.NavigateSessionTreeCommand))
	return request
}

// forkUIRequest creates one session fork request.
func forkUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetForkSession(new(uiv1.ForkSessionCommand))
	return request
}

// cloneUIRequest creates one session clone request.
func cloneUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetCloneSession(new(uiv1.CloneSessionCommand))
	return request
}

// entryLabelUIRequest creates one entry label request.
func entryLabelUIRequest() *uiv1.UIRequest {
	request := new(uiv1.UIRequest)
	request.SetSetEntryLabel(new(uiv1.SetEntryLabelCommand))
	return request
}
