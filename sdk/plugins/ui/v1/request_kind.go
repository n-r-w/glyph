package uiv1

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// requestKind identifies the payload contract for one UI-initiated operation.
type requestKind uint8

const (
	// requestSubmit identifies an agent run.
	requestSubmit requestKind = iota + 1
	// requestCancel identifies targeted cancellation.
	requestCancel
	// requestAuthentication identifies explicit authentication retry.
	requestAuthentication
	// requestSelectModel identifies model selection.
	requestSelectModel
	// requestSelectReasoning identifies reasoning selection.
	requestSelectReasoning
	// requestCreateSession identifies session creation.
	requestCreateSession
	// requestListSessions identifies session listing.
	requestListSessions
	// requestResumeSession identifies session resumption.
	requestResumeSession
	// requestSetSessionName identifies session naming.
	requestSetSessionName
	// requestGetSessionInfo identifies session information retrieval.
	requestGetSessionInfo
	// requestGetSessionTree identifies tree retrieval.
	requestGetSessionTree
	// requestNavigateSessionTree identifies tree navigation.
	requestNavigateSessionTree
	// requestForkSession identifies session forking.
	requestForkSession
	// requestCloneSession identifies session cloning.
	requestCloneSession
	// requestSetEntryLabel identifies entry label mutation.
	requestSetEntryLabel
)

// classifyUIRequest returns the exact request payload kind.
func classifyUIRequest(request *uiv1.UIRequest) (requestKind, error) {
	if request == nil {
		return 0, errors.New("UI operation request is required")
	}
	switch request.WhichRequest() {
	case uiv1.UIRequest_Submit_case:
		return requestSubmit, nil
	case uiv1.UIRequest_Cancel_case:
		return requestCancel, nil
	case uiv1.UIRequest_RetryAuthentication_case:
		return requestAuthentication, nil
	case uiv1.UIRequest_SelectModel_case:
		return requestSelectModel, nil
	case uiv1.UIRequest_SelectReasoningChoice_case:
		return requestSelectReasoning, nil
	case uiv1.UIRequest_CreateSession_case:
		return requestCreateSession, nil
	case uiv1.UIRequest_ListSessions_case:
		return requestListSessions, nil
	case uiv1.UIRequest_ResumeSession_case, uiv1.UIRequest_SetSessionName_case,
		uiv1.UIRequest_GetSessionInfo_case, uiv1.UIRequest_GetSessionTree_case,
		uiv1.UIRequest_NavigateSessionTree_case, uiv1.UIRequest_ForkSession_case,
		uiv1.UIRequest_CloneSession_case, uiv1.UIRequest_SetEntryLabel_case,
		uiv1.UIRequest_Request_not_set_case:
		return classifySessionUIRequest(request)
	default:
		return 0, errors.New("UI operation request payload is unknown")
	}
}

// classifySessionUIRequest returns one session-operation payload kind.
func classifySessionUIRequest(request *uiv1.UIRequest) (requestKind, error) {
	switch request.WhichRequest() {
	case uiv1.UIRequest_ResumeSession_case:
		return requestResumeSession, nil
	case uiv1.UIRequest_SetSessionName_case:
		return requestSetSessionName, nil
	case uiv1.UIRequest_GetSessionInfo_case:
		return requestGetSessionInfo, nil
	case uiv1.UIRequest_GetSessionTree_case:
		return requestGetSessionTree, nil
	case uiv1.UIRequest_NavigateSessionTree_case:
		return requestNavigateSessionTree, nil
	case uiv1.UIRequest_ForkSession_case:
		return requestForkSession, nil
	case uiv1.UIRequest_CloneSession_case:
		return requestCloneSession, nil
	case uiv1.UIRequest_SetEntryLabel_case:
		return requestSetEntryLabel, nil
	case uiv1.UIRequest_Request_not_set_case:
		return 0, errors.New("UI operation request payload is required")
	case uiv1.UIRequest_Submit_case, uiv1.UIRequest_Cancel_case,
		uiv1.UIRequest_RetryAuthentication_case, uiv1.UIRequest_SelectModel_case,
		uiv1.UIRequest_SelectReasoningChoice_case, uiv1.UIRequest_CreateSession_case,
		uiv1.UIRequest_ListSessions_case:
		return 0, errors.New("UI operation request payload is not a session request")
	default:
		return 0, errors.New("UI operation request payload is unknown")
	}
}

// validateHostPayload checks progress and completion against the initiating request kind.
func validateHostPayload(kind requestKind, event *uiv1.HostEvent) error {
	if event == nil {
		return errors.New("Host operation event is required")
	}
	if progress := event.GetProgress(); progress != nil {
		valid := kind == requestSubmit && progress.GetAgentEvent() != nil ||
			kind == requestAuthentication && progress.GetAuthorization() != nil
		if !valid {
			return fmt.Errorf("Host progress payload does not match UI request kind %d", kind)
		}
		if err := validateHostProgressFields(progress); err != nil {
			return err
		}
	}
	if completed := event.GetCompleted(); completed != nil {
		if !completedMatches(kind, completed) {
			return fmt.Errorf("Host completed payload does not match UI request kind %d", kind)
		}
		if err := validateHostCompletedFields(completed); err != nil {
			return err
		}
	}
	return nil
}

// completedMatches reports whether one completed payload matches its initiating request.
func completedMatches(kind requestKind, completed *uiv1.HostCompleted) bool {
	switch kind {
	case requestSubmit:
		return completed.GetSubmit() != nil
	case requestCancel:
		return completed.GetCancel() != nil
	case requestAuthentication:
		return completed.GetAuthentication() != nil
	case requestSelectModel, requestSelectReasoning:
		return completed.GetModelSelection() != nil
	case requestCreateSession, requestResumeSession:
		return completed.GetSessionChanged() != nil
	case requestListSessions:
		return completed.GetSessionList() != nil
	case requestSetSessionName, requestGetSessionInfo:
		return completed.GetSessionInformation() != nil
	case requestGetSessionTree:
		return completed.GetSessionTree() != nil
	case requestNavigateSessionTree:
		return completed.GetSessionTreeNavigation() != nil
	case requestForkSession:
		return completed.GetSessionForked() != nil
	case requestCloneSession:
		return completed.GetSessionCloned() != nil
	case requestSetEntryLabel:
		return completed.GetEntryLabelSet() != nil
	default:
		return false
	}
}
