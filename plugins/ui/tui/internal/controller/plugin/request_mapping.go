package plugin

import (
	"errors"
	"fmt"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// DecodeInitialization validates the startup payload before application admission.
func DecodeInitialization(request *uiv1.Initialization) (Initialization, error) {
	if request == nil {
		return Initialization{}, errors.New("initialization is required")
	}
	return mapInitialization(request)
}

// mapHostProgress decodes one running operation's typed input.
func mapHostProgress(progress *uiv1.HostProgress) (Payload, error) {
	if progress == nil {
		return Payload{}, errors.New("host progress is required")
	}
	switch progress.WhichProgress() {
	case uiv1.HostProgress_AgentEvent_case:
		update, err := DecodeLifecycle(progress.GetAgentEvent())
		return AgentPayload(update), err
	case uiv1.HostProgress_Authorization_case:
		authorization := progress.GetAuthorization()
		if authorization == nil || !authorization.HasUrl() {
			return Payload{}, errors.New("authorization URL is required")
		}
		return TextPayload(TextUpdate{Kind: TextAuthorization, Text: authorization.GetUrl(), FailureCode: ""}), nil
	case uiv1.HostProgress_SessionTreeNavigation_case:
		return mapTreeNavigationProgress(progress.GetSessionTreeNavigation())
	case uiv1.HostProgress_Progress_not_set_case:
		return Payload{}, errors.New("host progress payload is required")
	default:
		return Payload{}, errors.New("host progress payload is unknown")
	}
}

// DecodeCompleted decodes one terminal result without deciding its display effect.
func DecodeCompleted(completed *uiv1.HostCompleted) (Payload, bool, error) {
	if completed == nil {
		return Payload{}, false, errors.New("host completion is required")
	}
	if update, handled, err := mapTreeRequest(completed); handled {
		return update, true, err
	}
	if update, handled, err := mapSessionRequest(completed); handled {
		return update, true, err
	}
	if changed := completed.GetModelSelection(); changed != nil {
		selection, err := mapModelSelection(changed.GetSelection())
		if err != nil {
			return Payload{}, true, err
		}
		return SelectionPayload(selection), true, nil
	}
	switch completed.WhichCompleted() {
	case uiv1.HostCompleted_Submit_case:
		return NewPayload(PayloadSettled), true, nil
	case uiv1.HostCompleted_Authentication_case, uiv1.HostCompleted_Cancel_case:
		return Payload{}, false, nil
	case uiv1.HostCompleted_ModelSelection_case, uiv1.HostCompleted_SessionChanged_case,
		uiv1.HostCompleted_SessionList_case, uiv1.HostCompleted_SessionInformation_case,
		uiv1.HostCompleted_SessionTree_case, uiv1.HostCompleted_SessionTreeNavigation_case,
		uiv1.HostCompleted_SessionForked_case, uiv1.HostCompleted_SessionCloned_case,
		uiv1.HostCompleted_EntryLabelSet_case:
		return Payload{}, false, errors.New("host completion payload was not mapped")
	case uiv1.HostCompleted_Completed_not_set_case:
		return Payload{}, false, errors.New("host completion payload is required")
	default:
		return Payload{}, false, errors.New("host completion payload is unknown")
	}
}

// extensionIssueFormat identifies the source of one nonterminal observer issue.
const extensionIssueFormat = "extension %s handler %s [%s]: %s"

// DecodeConnectionEvent decodes one unsolicited Host update.
func DecodeConnectionEvent(connection *uiv1.HostConnectionEvent) (Payload, error) {
	if connection == nil {
		return Payload{}, errors.New("host connection event is required")
	}
	switch connection.WhichEvent() {
	case uiv1.HostConnectionEvent_Information_case:
		return mapInformation(connection.GetInformation())
	case uiv1.HostConnectionEvent_Error_case:
		return mapConnectionError(connection.GetError())
	case uiv1.HostConnectionEvent_SessionEntryAdded_case:
		return mapSessionEntryAdded(connection.GetSessionEntryAdded())
	case uiv1.HostConnectionEvent_ExtensionIssue_case:
		return mapExtensionIssue(connection.GetExtensionIssue())
	case uiv1.HostConnectionEvent_AvailabilityChanged_case:
		availability, err := mapAvailability(connection.GetAvailabilityChanged().GetAvailability())
		if err != nil {
			return Payload{}, err
		}
		return AvailabilityPayload(availability), nil
	case uiv1.HostConnectionEvent_Event_not_set_case:
		return Payload{}, errors.New("host connection event payload is required")
	default:
		return Payload{}, errors.New("host connection event payload is unknown")
	}
}

// mapInformation validates an informational connection update.
func mapInformation(information *uiv1.Information) (Payload, error) {
	if information == nil || !information.HasText() {
		return Payload{}, errors.New("information text is required")
	}
	return TextPayload(TextUpdate{Kind: TextInformation, Text: information.GetText(), FailureCode: ""}), nil
}

// mapConnectionError retains a validated connection failure's category and complete diagnostic text.
func mapConnectionError(failure *uiv1.Error) (Payload, error) {
	if failure == nil || !failure.HasCode() || failure.GetCode() == "" ||
		!failure.HasText() || failure.GetText() == "" {
		return Payload{}, errors.New("connection error category and text are required")
	}
	return TextPayload(TextUpdate{Kind: TextError, Text: failure.GetText(), FailureCode: failure.GetCode()}), nil
}

// mapExtensionIssue validates and identifies one nonterminal observer issue.
func mapExtensionIssue(issue *uiv1.ExtensionIssue) (Payload, error) {
	if issue == nil || issue.GetExtensionId() == "" || issue.GetHandlerId() == "" ||
		issue.GetCode() == "" || issue.GetText() == "" {
		return Payload{}, errors.New("extension issue identity, code, and text are required")
	}
	return TextPayload(TextUpdate{Kind: TextError, FailureCode: issue.GetCode(), Text: fmt.Sprintf(
		extensionIssueFormat, issue.GetExtensionId(), issue.GetHandlerId(), issue.GetCode(), issue.GetText(),
	)}), nil
}

// mapSessionEntryAdded validates a committed extension-message update.
func mapSessionEntryAdded(added *uiv1.SessionEntryAdded) (Payload, error) {
	if added == nil || added.GetEntry() == nil {
		return Payload{}, errors.New("added session entry is required")
	}
	entry, err := mapSessionTreeEntry(added.GetEntry())
	if err != nil {
		return Payload{}, err
	}
	transcript := make([]Transcript, 0, 1)
	message := added.GetEntry().GetExtensionMessage()
	if message != nil && message.GetVisibility() == uiv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE {
		transcript = append(transcript, Transcript{
			Kind: TranscriptUser, ToolName: mo.None[string](), Status: mo.None[string](),
			Text: mo.Some(message.GetText()), Contents: mo.Some([]Content{{
				Text: mo.Some(message.GetText()), MediaType: mo.None[string](), Data: mo.None[[]byte](),
			}}),
		})
	}
	return TreePayload(TreeUpdate{
		Kind: TreeEntryAdded, Tree: mo.None[SessionTree](), NavigationStatus: TreeNavigationUnspecified,
		SessionInfo: mo.None[SessionInfo](), Transcript: transcript, NextInput: mo.None[string](),
		Issues: nil, AddedEntry: mo.Some(entry),
	}), nil
}

// mapSessionRequest validates session results without interpreting editor or selector state.
func mapSessionRequest(request *uiv1.HostCompleted) (Payload, bool, error) {
	if listed := request.GetSessionList(); listed != nil {
		summaries := make([]SessionSummary, 0, len(listed.GetSessions()))
		for _, value := range listed.GetSessions() {
			mapped, err := mapSessionSummary(value)
			if err != nil {
				return Payload{}, true, err
			}
			summaries = append(summaries, mapped)
		}
		return SessionPayload(SessionUpdate{
			Kind: SessionListed, Info: mo.None[SessionInfo](), Sessions: summaries,
			Transcript: nil, Statistics: mo.None[SessionStatistics](),
		}), true, nil
	}
	if changed := request.GetSessionChanged(); changed != nil {
		info, err := mapSessionInfo(changed.GetInfo())
		if err != nil {
			return Payload{}, true, err
		}
		restored, err := mapRestoredTranscript(changed.GetEntries())
		if err != nil {
			return Payload{}, true, err
		}
		return SessionPayload(SessionUpdate{
			Kind: SessionChanged, Info: mo.Some(info), Sessions: nil,
			Transcript: restored, Statistics: mo.None[SessionStatistics](),
		}), true, nil
	}
	if information := request.GetSessionInformation(); information != nil {
		info, err := mapSessionInfo(information.GetInfo())
		if err != nil {
			return Payload{}, true, err
		}
		statistics, err := mapSessionStatistics(information.GetStatistics())
		if err != nil {
			return Payload{}, true, err
		}
		return SessionPayload(SessionUpdate{
			Kind: SessionInformation, Info: mo.Some(info), Sessions: nil,
			Transcript: nil, Statistics: mo.Some(statistics),
		}), true, nil
	}
	return Payload{}, false, nil
}
