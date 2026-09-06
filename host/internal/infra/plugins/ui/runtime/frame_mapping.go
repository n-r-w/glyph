package runtime

import (
	"bytes"
	"errors"
	"fmt"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/lo"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapFrame converts one provider-neutral frame without exposing internal objects.
func mapFrame(frame controllerui.Frame) (*uiv1.OpenRequest, error) {
	if frame.Kind == controllerui.FrameSessionTreeNavigationProgress {
		progress, present := frame.TreeNavigationProgress.Get()
		if !present {
			return nil, errors.New("map UI tree navigation progress: payload is required")
		}
		mapped, err := mapTreeNavigationProgress(progress)
		if err != nil {
			return nil, err
		}
		payload := new(uiv1.HostProgress)
		payload.SetSessionTreeNavigation(mapped)
		return progressRequest(payload), nil
	}
	if completed, handled, err := mapTreeFrame(frame); handled {
		return completedRequest(completed), err
	}
	if completed, handled, err := mapSessionFrame(frame); handled {
		return completedRequest(completed), err
	}
	switch frame.Kind {
	case controllerui.FrameLifecycle:
		_, present := frame.Lifecycle.Get()
		if !present {
			return nil, errors.New("map UI frame: lifecycle payload is required")
		}
		return mapLifecycleFrame(frame)
	case controllerui.FrameAuthorization:
		authorizationURL, present := frame.AuthorizationURL.Get()
		if !present {
			return nil, errors.New("map UI frame: authorization payload is required")
		}
		progress := new(uiv1.HostProgress)
		progress.SetAuthorization(uiv1.AuthorizationRequest_builder{Url: new(authorizationURL)}.Build())
		return progressRequest(progress), nil
	case controllerui.FrameSubmitCompleted:
		completed := new(uiv1.HostCompleted)
		completed.SetSubmit(new(uiv1.SubmitCompleted))
		return completedRequest(completed), nil
	case controllerui.FrameAuthenticationCompleted:
		completed := new(uiv1.HostCompleted)
		completed.SetAuthentication(new(uiv1.AuthenticationCompleted))
		return completedRequest(completed), nil
	case controllerui.FrameModelSelectionChanged:
		selection, present := frame.ModelSelection.Get()
		if !present {
			return nil, errors.New("map UI frame: model selection payload is required")
		}
		completed := new(uiv1.HostCompleted)
		completed.SetModelSelection(uiv1.ModelSelectionChanged_builder{Selection: mapModelSelection(selection)}.Build())
		return completedRequest(completed), nil
	case controllerui.FrameSessionList, controllerui.FrameSessionChanged, controllerui.FrameSessionInformation,
		controllerui.FrameSessionTree, controllerui.FrameSessionTreeNavigationProgress,
		controllerui.FrameSessionTreeNavigation, controllerui.FrameSessionForked,
		controllerui.FrameSessionCloned, controllerui.FrameEntryLabelSet:
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
func mapSessionFrame(frame controllerui.Frame) (*uiv1.HostCompleted, bool, error) {
	request := new(uiv1.HostCompleted)
	switch frame.Kind {
	case controllerui.FrameSessionList:
		mapped := lo.Map(frame.Sessions, func(value controllerui.SessionListItem, _ int) *uiv1.SessionSummary {
			return mapSessionSummary(value)
		})
		request.SetSessionList(uiv1.SessionList_builder{Sessions: mapped}.Build())
		return request, true, nil
	case controllerui.FrameSessionChanged, controllerui.FrameSessionForked, controllerui.FrameSessionCloned:
		return mapReplacementSessionFrame(request, frame)
	case controllerui.FrameSessionInformation:
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
	case controllerui.FrameLifecycle, controllerui.FrameAuthorization,
		controllerui.FrameModelSelectionChanged,
		controllerui.FrameSessionTree, controllerui.FrameSessionTreeNavigationProgress,
		controllerui.FrameSessionTreeNavigation,
		controllerui.FrameEntryLabelSet, controllerui.FrameSubmitCompleted, controllerui.FrameAuthenticationCompleted:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapReplacementSessionFrame maps create, resume, fork, and clone replacement state.
func mapReplacementSessionFrame(
	request *uiv1.HostCompleted,
	frame controllerui.Frame,
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
	if frame.Kind == controllerui.FrameSessionForked {
		nextInput, nextInputPresent := frame.NextInput.Get()
		if !nextInputPresent {
			return nil, true, errors.New("map UI fork frame: next input is required")
		}
		request.SetSessionForked(uiv1.SessionForked_builder{Session: changed, NextInput: new(nextInput)}.Build())
		return request, true, nil
	}
	if frame.Kind == controllerui.FrameSessionCloned {
		request.SetSessionCloned(uiv1.SessionCloned_builder{Session: changed}.Build())
		return request, true, nil
	}
	request.SetSessionChanged(changed)
	return request, true, nil
}

func mapRestoredSessionEntries(entries []controllerui.SessionEntry) ([]*uiv1.SessionEntry, error) {
	return lo.MapErr(entries, func(entry controllerui.SessionEntry, index int) (*uiv1.SessionEntry, error) {
		wire := new(uiv1.SessionEntry)
		wire.SetId(entry.ID)
		wire.SetCreatedTime(timestamppb.New(entry.CreatedAt))
		switch entry.Kind {
		case controllerui.SessionEntryUser:
			user, present := entry.User.Get()
			if !present {
				return nil, fmt.Errorf("map restored session entry %d: user payload is missing", index)
			}
			mapped, err := mapRestoredUserMessage(user)
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", index, err)
			}
			wire.SetUser(mapped)
		case controllerui.SessionEntryModel:
			response, _ := entry.Model.Get()
			mapped, err := mapModelResponse(response)
			if err != nil {
				return nil, fmt.Errorf("map restored session entry %d: %w", index, err)
			}
			wire.SetModel(mapped)
		case controllerui.SessionEntryToolResult:
			result, _ := entry.ToolResult.Get()
			wire.SetToolResult(uiv1.ToolResult_builder{
				CallId: new(result.CallID), ToolName: new(result.ToolName),
				Contents: mapToolResultContents(result.Contents), IsError: new(result.IsError),
			}.Build())
		case controllerui.SessionEntryExtensionMessage:
			message, present := entry.ExtensionMessage.Get()
			if !present {
				return nil, fmt.Errorf("map restored session entry %d: extension message is missing", index)
			}
			wire.SetExtensionMessage(mapExtensionMessage(message))
		case controllerui.SessionEntryBranchSummary:
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
func mapInitializationFrame(initialization Initialization) (*uiv1.OpenRequest, error) {
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
func mapLifecycleFrame(frame controllerui.Frame) (*uiv1.OpenRequest, error) {
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
