package plugin

import (
	"errors"

	"github.com/samber/mo"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// mapRequest validates and projects one public UI frame into presentation state.
func mapRequest(request *uiv1.OpenRequest) (presentationdomain.Event, error) {
	if request == nil {
		return presentationdomain.Event{}, errors.New("frame is nil")
	}
	if initialization := request.GetInitialization(); initialization != nil {
		return mapInitialization(initialization)
	}
	if lifecycle := request.GetLifecycle(); lifecycle != nil {
		return mapLifecycle(lifecycle)
	}
	if event, handled, err := mapSessionRequest(request); handled {
		return event, err
	}
	if event, handled, err := mapTextRequest(request); handled {
		return event, err
	}
	if safeError := request.GetError(); safeError != nil {
		if !safeError.HasText() {
			return presentationdomain.Event{}, errors.New("error text is required")
		}
		if !safeError.HasRetryAuthentication() {
			return presentationdomain.Event{}, errors.New("error retry authentication is required")
		}
		availability := mo.None[presentationdomain.Availability]()
		if safeError.GetRetryAuthentication() {
			availability = mo.Some(presentationdomain.AvailabilityAuthenticationFailed)
		}
		return presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventError,
			Startup:              nil,
			Extensions:           nil,
			Availability:         availability,
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
			Text:                 mo.Some(safeError.GetText()),
			Contents:             mo.None[[]presentationdomain.Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		}, nil
	}
	if changed := request.GetModelSelectionChanged(); changed != nil {
		selection, err := mapModelSelection(changed.GetSelection())
		if err != nil {
			return presentationdomain.Event{}, err
		}
		return presentationdomain.Event{
			RestoredTranscript:   nil,
			Kind:                 presentationdomain.EventModelSelectionChanged,
			Startup:              nil,
			Extensions:           nil,
			Availability:         mo.None[presentationdomain.Availability](),
			Position:             mo.None[int](),
			ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
			ModelResponseContent: nil,
			ToolCallID:           mo.None[string](),
			ToolName:             mo.None[string](),
			Status:               mo.None[string](),
			Stream:               mo.None[presentationdomain.OutputStream](),
			Text:                 mo.None[string](),
			Contents:             mo.None[[]presentationdomain.Content](),
			ErrorText:            mo.None[string](),
			ExitCode:             mo.None[int](),
			Failure:              mo.None[bool](),
			ToolCall:             mo.None[presentationdomain.ToolCallState](),
			Models:               nil,
			ModelSelection:       mo.Some(selection),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
			Sessions:             nil,
			SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		}, nil
	}
	return presentationdomain.Event{}, errors.New("frame content is missing")
}

// mapTextRequest maps authorization and information payloads that share text-only presentation state.
func mapTextRequest(request *uiv1.OpenRequest) (presentationdomain.Event, bool, error) {
	var kind presentationdomain.EventKind
	var text string
	if authorization := request.GetAuthorization(); authorization != nil {
		if !authorization.HasUrl() {
			return presentationdomain.Event{}, true, errors.New("authorization URL is required")
		}
		kind = presentationdomain.EventAuthorization
		text = authorization.GetUrl()
	} else if information := request.GetInformation(); information != nil {
		if !information.HasText() {
			return presentationdomain.Event{}, true, errors.New("information text is required")
		}
		kind = presentationdomain.EventInformation
		text = information.GetText()
	} else {
		return presentationdomain.Event{}, false, nil
	}
	return presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 kind,
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.Some(text),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	}, true, nil
}

// mapSessionRequest validates lifecycle frames before they enter TUI state.
func mapSessionRequest(request *uiv1.OpenRequest) (presentationdomain.Event, bool, error) {
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
