package plugin

import (
	"errors"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// mapInitializationRequest validates and projects the startup payload.
func mapInitializationRequest(request *uiv1.Initialization) (presentationdomain.Event, error) {
	if request == nil {
		return presentationdomain.Event{}, errors.New("initialization is required")
	}
	return mapInitialization(request)
}

// mapProgress projects one Host operation progress event.
func mapHostProgress(progress *uiv1.HostProgress) (presentationdomain.Event, error) {
	if progress == nil {
		return presentationdomain.Event{}, errors.New("host progress is required")
	}
	switch progress.WhichProgress() {
	case uiv1.HostProgress_AgentEvent_case:
		return mapLifecycle(progress.GetAgentEvent())
	case uiv1.HostProgress_Authorization_case:
		authorization := progress.GetAuthorization()
		if authorization == nil || !authorization.HasUrl() {
			return presentationdomain.Event{}, errors.New("authorization URL is required")
		}
		return textEvent(presentationdomain.EventAuthorization, authorization.GetUrl()), nil
	case uiv1.HostProgress_Progress_not_set_case:
		return presentationdomain.Event{}, errors.New("host progress payload is required")
	default:
		return presentationdomain.Event{}, errors.New("host progress payload is unknown")
	}
}

// mapCompleted projects one Host operation completed payload.
func mapCompleted(completed *uiv1.HostCompleted) (presentationdomain.Event, bool, error) {
	if completed == nil {
		return presentationdomain.Event{}, false, errors.New("host completion is required")
	}
	if event, handled, err := mapTreeRequest(completed); handled {
		return event, true, err
	}
	if event, handled, err := mapSessionRequest(completed); handled {
		return event, true, err
	}
	if changed := completed.GetModelSelection(); changed != nil {
		selection, err := mapModelSelection(changed.GetSelection())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		event := emptyEvent(presentationdomain.EventModelSelectionChanged)
		event.ModelSelection = mo.Some(selection)
		return event, true, nil
	}
	switch completed.WhichCompleted() {
	case uiv1.HostCompleted_Submit_case:
		return emptyEvent(presentationdomain.EventAgentSettled), true, nil
	case uiv1.HostCompleted_Authentication_case, uiv1.HostCompleted_Cancel_case:
		return presentationdomain.Event{}, false, nil
	case uiv1.HostCompleted_ModelSelection_case, uiv1.HostCompleted_SessionChanged_case,
		uiv1.HostCompleted_SessionList_case, uiv1.HostCompleted_SessionInformation_case,
		uiv1.HostCompleted_SessionTree_case, uiv1.HostCompleted_SessionTreeNavigation_case,
		uiv1.HostCompleted_SessionForked_case, uiv1.HostCompleted_SessionCloned_case,
		uiv1.HostCompleted_EntryLabelSet_case:
		return presentationdomain.Event{}, false, errors.New("host completion payload was not mapped")
	case uiv1.HostCompleted_Completed_not_set_case:
		return presentationdomain.Event{}, false, errors.New("host completion payload is required")
	default:
		return presentationdomain.Event{}, false, errors.New("host completion payload is unknown")
	}
}

// mapConnectionEvent projects one host connection event.
func mapConnectionEvent(connection *uiv1.HostConnectionEvent) (presentationdomain.Event, error) {
	if connection == nil {
		return presentationdomain.Event{}, errors.New("host connection event is required")
	}
	switch connection.WhichEvent() {
	case uiv1.HostConnectionEvent_Information_case:
		information := connection.GetInformation()
		if information == nil || !information.HasText() {
			return presentationdomain.Event{}, errors.New("information text is required")
		}
		return textEvent(presentationdomain.EventInformation, information.GetText()), nil
	case uiv1.HostConnectionEvent_Error_case:
		failure := connection.GetError()
		if failure == nil || !failure.HasCode() || failure.GetCode() == "" ||
			!failure.HasText() || failure.GetText() == "" {
			return presentationdomain.Event{}, errors.New("connection error category and text are required")
		}
		return textEvent(presentationdomain.EventError, failure.GetText()), nil
	case uiv1.HostConnectionEvent_AvailabilityChanged_case:
		availability, err := mapAvailability(connection.GetAvailabilityChanged().GetAvailability())
		if err != nil {
			return presentationdomain.Event{}, err
		}
		event := emptyEvent(presentationdomain.EventAvailability)
		event.Availability = mo.Some(availability)
		return event, nil
	case uiv1.HostConnectionEvent_Event_not_set_case:
		return presentationdomain.Event{}, errors.New("host connection event payload is required")
	default:
		return presentationdomain.Event{}, errors.New("host connection event payload is unknown")
	}
}

// textEvent creates one complete text-only presentation event.
func textEvent(kind presentationdomain.EventKind, text string) presentationdomain.Event {
	event := emptyEvent(kind)
	event.Text = mo.Some(text)
	return event
}

// emptyEvent creates one fully initialized presentation event.
func emptyEvent(kind presentationdomain.EventKind) presentationdomain.Event {
	return presentationdomain.Event{
		RestoredTranscript: nil, Kind: kind, Startup: nil, Extensions: nil,
		Availability: mo.None[presentationdomain.Availability](), Position: mo.None[int](),
		ModelContentKind: mo.None[presentationdomain.ModelContentKind](), ModelResponseContent: nil,
		ToolCallID: mo.None[string](), ToolName: mo.None[string](), Status: mo.None[string](),
		Stream: mo.None[presentationdomain.OutputStream](), Text: mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](), ErrorText: mo.None[string](),
		ExitCode: mo.None[int](), Failure: mo.None[bool](), ToolCall: mo.None[presentationdomain.ToolCallState](),
		Models: nil, ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:         mo.None[presentationdomain.TreeEvent](),
	}
}

// mapSessionRequest validates lifecycle frames before they enter TUI state.
func mapSessionRequest(request *uiv1.HostCompleted) (presentationdomain.Event, bool, error) {
	if listed := request.GetSessionList(); listed != nil {
		summaries := make([]presentationdomain.SessionSummary, 0, len(listed.GetSessions()))
		for _, value := range listed.GetSessions() {
			mapped, err := mapSessionSummary(value)
			if err != nil {
				return presentationdomain.Event{}, true, err
			}
			summaries = append(summaries, mapped)
		}
		return sessionEvent(
			presentationdomain.EventSessionList, mo.None[presentationdomain.SessionInfo](), summaries, nil,
			mo.None[presentationdomain.SessionStatistics](),
		), true, nil
	}
	if changed := request.GetSessionChanged(); changed != nil {
		info, err := mapSessionInfo(changed.GetInfo())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		restored, err := mapRestoredTranscript(changed.GetEntries())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		return sessionEvent(
			presentationdomain.EventSessionChanged, mo.Some(info), nil, restored,
			mo.None[presentationdomain.SessionStatistics](),
		), true, nil
	}
	if information := request.GetSessionInformation(); information != nil {
		info, err := mapSessionInfo(information.GetInfo())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		statistics, err := mapSessionStatistics(information.GetStatistics())
		if err != nil {
			return presentationdomain.Event{}, true, err
		}
		return sessionEvent(
			presentationdomain.EventSessionInformation, mo.Some(info), nil, nil, mo.Some(statistics),
		), true, nil
	}
	return presentationdomain.Event{}, false, nil
}
