package runtime

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/samber/lo"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapFrame converts one provider-neutral frame without exposing internal objects.
//
//nolint:gocyclo // The closed frame union requires one explicit mapping for each public payload.
func mapFrame(frame domainui.Frame) (*uiv1.OpenRequest, error) {
	if completed, handled, err := mapTreeFrame(frame); handled {
		return completedRequest(completed), err
	}
	if completed, handled, err := mapSessionFrame(frame); handled {
		return completedRequest(completed), err
	}
	switch frame.Kind {
	case domainui.FrameInitialization:
		return mapInitializationFrame(frame)
	case domainui.FrameLifecycle:
		lifecycle, present := frame.Lifecycle.Get()
		if !present {
			return nil, errors.New("map UI frame: lifecycle payload is required")
		}
		if lifecycle.Type == domainui.LifecycleAvailabilityChanged {
			availability, availabilityPresent := lifecycle.Availability.Get()
			if !availabilityPresent {
				return nil, errors.New("map UI frame: availability payload is required")
			}
			connection := new(uiv1.HostConnectionEvent)
			connection.SetAvailabilityChanged(uiv1.AvailabilityChanged_builder{
				Availability: new(mapAvailability(availability)),
			}.Build())
			return connectionRequest(connection), nil
		}
		return mapLifecycleFrame(frame)
	case domainui.FrameAuthorization:
		authorizationURL, present := frame.AuthorizationURL.Get()
		if !present {
			return nil, errors.New("map UI frame: authorization payload is required")
		}
		progress := new(uiv1.HostProgress)
		progress.SetAuthorization(uiv1.AuthorizationRequest_builder{Url: new(authorizationURL)}.Build())
		return progressRequest(progress), nil
	case domainui.FrameInformation:
		text, present := frame.Text.Get()
		if !present {
			return nil, errors.New("map UI frame: information payload is required")
		}
		connection := new(uiv1.HostConnectionEvent)
		connection.SetInformation(uiv1.Information_builder{Text: new(text)}.Build())
		return connectionRequest(connection), nil
	case domainui.FrameError:
		text, present := frame.Text.Get()
		if !present {
			return nil, errors.New("map UI frame: error payload is required")
		}
		code, codePresent := frame.ErrorCode.Get()
		if !codePresent || code == "" {
			return nil, errors.New("map UI frame: error category is required")
		}
		connection := new(uiv1.HostConnectionEvent)
		connection.SetError(uiv1.Error_builder{Code: new(code), Text: new(text)}.Build())
		return connectionRequest(connection), nil
	case domainui.FrameSubmitCompleted:
		completed := new(uiv1.HostCompleted)
		completed.SetSubmit(new(uiv1.SubmitCompleted))
		return completedRequest(completed), nil
	case domainui.FrameAuthenticationCompleted:
		completed := new(uiv1.HostCompleted)
		completed.SetAuthentication(new(uiv1.AuthenticationCompleted))
		return completedRequest(completed), nil
	case domainui.FrameModelSelectionChanged:
		selection, present := frame.ModelSelection.Get()
		if !present {
			return nil, errors.New("map UI frame: model selection payload is required")
		}
		completed := new(uiv1.HostCompleted)
		completed.SetModelSelection(uiv1.ModelSelectionChanged_builder{Selection: mapModelSelection(selection)}.Build())
		return completedRequest(completed), nil
	case domainui.FrameSessionList, domainui.FrameSessionChanged, domainui.FrameSessionInformation,
		domainui.FrameSessionTree, domainui.FrameSessionTreeNavigation, domainui.FrameSessionForked,
		domainui.FrameSessionCloned, domainui.FrameEntryLabelSet:
		return nil, errors.New("map UI frame: completed payload was not mapped")
	default:
		return nil, errors.New("map UI frame: payload is required")
	}
}

// completedRequest wraps one completed operation payload.
func completedRequest(completed *uiv1.HostCompleted) *uiv1.OpenRequest {
	event := new(uiv1.HostEvent)
	event.SetCompleted(completed)
	request := new(uiv1.OpenRequest)
	request.SetEvent(event)
	return request
}

// progressRequest wraps one operation progress payload.
func progressRequest(progress *uiv1.HostProgress) *uiv1.OpenRequest {
	event := new(uiv1.HostEvent)
	event.SetProgress(progress)
	request := new(uiv1.OpenRequest)
	request.SetEvent(event)
	return request
}

// connectionRequest wraps one Host connection event.
func connectionRequest(connection *uiv1.HostConnectionEvent) *uiv1.OpenRequest {
	request := new(uiv1.OpenRequest)
	request.SetConnectionEvent(connection)
	return request
}

// mapSessionFrame maps lifecycle frames to protobuf payloads without losing optional fields.
func mapSessionFrame(frame domainui.Frame) (*uiv1.HostCompleted, bool, error) {
	request := new(uiv1.HostCompleted)
	switch frame.Kind {
	case domainui.FrameSessionList:
		mapped := lo.Map(frame.Sessions, func(value session.Summary, _ int) *uiv1.SessionSummary {
			return mapSessionSummary(value)
		})
		request.SetSessionList(uiv1.SessionList_builder{Sessions: mapped}.Build())
		return request, true, nil
	case domainui.FrameSessionChanged, domainui.FrameSessionForked, domainui.FrameSessionCloned:
		return mapReplacementSessionFrame(request, frame)
	case domainui.FrameSessionInformation:
		info, present := frame.SessionInfo.Get()
		if !present {
			return nil, true, errors.New("map UI frame: session information is required")
		}
		statistics, statisticsPresent := frame.SessionStatistics.Get()
		if !statisticsPresent {
			return nil, true, errors.New("map UI frame: session statistics are required")
		}
		request.SetSessionInformation(uiv1.SessionInformation_builder{
			Info: mapSessionInfo(info), Statistics: mapSessionStatistics(statistics),
		}.Build())
		return request, true, nil
	case domainui.FrameInitialization, domainui.FrameLifecycle, domainui.FrameAuthorization,
		domainui.FrameInformation, domainui.FrameError, domainui.FrameModelSelectionChanged,
		domainui.FrameSessionTree, domainui.FrameSessionTreeNavigation,
		domainui.FrameEntryLabelSet, domainui.FrameSubmitCompleted, domainui.FrameAuthenticationCompleted:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapReplacementSessionFrame maps create, resume, fork, and clone replacement state.
func mapReplacementSessionFrame(
	request *uiv1.HostCompleted,
	frame domainui.Frame,
) (*uiv1.HostCompleted, bool, error) {
	info, present := frame.SessionInfo.Get()
	if !present {
		return nil, true, errors.New("map UI frame: session information is required")
	}
	entries, err := mapRestoredSessionEntries(frame.SessionEntries)
	if err != nil {
		return nil, true, err
	}
	changed := uiv1.SessionChanged_builder{Info: mapSessionInfo(info), Entries: entries}.Build()
	if frame.Kind == domainui.FrameSessionForked {
		nextInput, nextInputPresent := frame.Text.Get()
		if !nextInputPresent {
			return nil, true, errors.New("map UI fork frame: next input is required")
		}
		request.SetSessionForked(uiv1.SessionForked_builder{Session: changed, NextInput: new(nextInput)}.Build())
		return request, true, nil
	}
	if frame.Kind == domainui.FrameSessionCloned {
		request.SetSessionCloned(uiv1.SessionCloned_builder{Session: changed}.Build())
		return request, true, nil
	}
	request.SetSessionChanged(changed)
	return request, true, nil
}

func mapRestoredSessionEntries(entries []domainui.SessionEntry) ([]*uiv1.SessionEntry, error) {
	return lo.MapErr(entries, func(entry domainui.SessionEntry, index int) (*uiv1.SessionEntry, error) {
		wire := new(uiv1.SessionEntry)
		wire.SetId(entry.ID)
		wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
		switch entry.Kind {
		case domainui.SessionEntryUser:
			user, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("map restored session entry %d: user payload is missing", index)
			}
			mapped, err := mapRestoredUserMessage(user)
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", index, err)
			}
			wire.SetUser(mapped)
		case domainui.SessionEntryModel:
			response, _ := entry.Model.Get()
			mapped, err := mapModelResponse(response)
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", index, err)
			}
			wire.SetModel(mapped)
		case domainui.SessionEntryToolResult:
			result, _ := entry.ToolResult.Get()
			wire.SetToolResult(uiv1.ToolResult_builder{
				CallId: new(result.CallID), ToolName: new(result.ToolName),
				Contents: mapToolResultContents(result.Contents), IsError: new(result.IsError),
			}.Build())
		case domainui.SessionEntryBranchSummary:
			summary, present := entry.BranchSummary.Get()
			if !present {
				return nil, fmt.Errorf("map restored session entry %d: branch summary is missing", index)
			}
			mapped, err := mapBranchSummary(summary)
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", index, err)
			}
			wire.SetBranchSummary(mapped)
		}
		return wire, nil
	})
}

// mapRestoredUserMessage maps all content in one restored user message.
func mapRestoredUserMessage(user model.Message) (*uiv1.UserMessage, error) {
	content, err := lo.MapErr(user.Content, mapRestoredUserContent)
	if err != nil {
		return nil, err
	}
	return uiv1.UserMessage_builder{Content: content}.Build(), nil
}

// mapRestoredUserContent validates and maps one restored user content item.
func mapRestoredUserContent(item model.InputContent, index int) (*uiv1.UserContent, error) {
	wire := new(uiv1.UserContent)
	switch item.Kind {
	case model.InputContentText:
		text, hasText := item.Text.Get()
		if !hasText || item.MediaType.IsSome() || item.Data.IsSome() {
			return nil, fmt.Errorf("map restored user content %d: invalid text payload", index)
		}
		wire.SetText(text)
	case model.InputContentImage:
		mediaType, hasMediaType := item.MediaType.Get()
		data, hasData := item.Data.Get()
		if item.Text.IsSome() || !hasMediaType || !hasData {
			return nil, fmt.Errorf("map restored user content %d: invalid image payload", index)
		}
		image := uiv1.UserImage_builder{MediaType: new(mediaType), Data: nil}.Build()
		image.SetData(bytes.Clone(data))
		wire.SetImage(image)
	default:
		return nil, fmt.Errorf("map restored user content %d: unknown kind %d", index, item.Kind)
	}
	return wire, nil
}

// mapInitializationFrame validates and maps the selected initialization payload.
func mapInitializationFrame(frame domainui.Frame) (*uiv1.OpenRequest, error) {
	initialization, present := frame.Initialization.Get()
	if !present {
		return nil, errors.New("map UI frame: initialization payload is required")
	}
	mapped, err := mapInitialization(initialization)
	if err != nil {
		return nil, err
	}
	hostRequest := new(uiv1.HostRequest)
	hostRequest.SetInitialize(mapped)
	request := new(uiv1.OpenRequest)
	request.SetRequest(hostRequest)
	return request, nil
}

// mapLifecycleFrame validates and maps one agent progress payload.
func mapLifecycleFrame(frame domainui.Frame) (*uiv1.OpenRequest, error) {
	lifecycle, present := frame.Lifecycle.Get()
	if !present {
		return nil, errors.New("map UI frame: lifecycle payload is required")
	}
	mapped, err := mapLifecycle(lifecycle)
	if err != nil {
		return nil, err
	}
	progress := new(uiv1.HostProgress)
	progress.SetAgentEvent(mapped)
	return progressRequest(progress), nil
}
