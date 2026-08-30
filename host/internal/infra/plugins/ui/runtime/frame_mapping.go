package runtime

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// mapFrame converts one provider-neutral frame without exposing internal objects.
func mapFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	if request, handled, err := mapTreeFrame(frame); handled {
		return request, err
	}
	if request, handled, err := mapSessionFrame(frame); handled {
		return request, err
	}
	switch frame.Kind {
	case domainui.FrameInitialization:
		return mapInitializationFrame(frame)
	case domainui.FrameLifecycle:
		return mapLifecycleFrame(frame)
	case domainui.FrameAuthorization:
		authorizationURL, present := frame.AuthorizationURL.Get()
		if !present {
			return nil, errors.New("map UI frame: authorization payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetAuthorization(uipb.AuthorizationRequest_builder{
			Url: new(authorizationURL),
		}.Build())
		return request, nil
	case domainui.FrameInformation:
		text, present := frame.Text.Get()
		if !present {
			return nil, errors.New("map UI frame: information payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetInformation(uipb.Information_builder{
			Text: new(text),
		}.Build())
		return request, nil
	case domainui.FrameError:
		text, hasText := frame.Text.Get()
		retryAuthentication, hasRetryAuthentication := frame.RetryAuthentication.Get()
		if !hasText || !hasRetryAuthentication {
			return nil, errors.New("map UI frame: error payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetError(uipb.Error_builder{
			Text:                new(text),
			RetryAuthentication: new(retryAuthentication),
		}.Build())
		return request, nil
	case domainui.FrameModelSelectionChanged:
		selection, present := frame.ModelSelection.Get()
		if !present {
			return nil, errors.New("map UI frame: model selection payload is required")
		}
		request := &uipb.OpenRequest{}
		request.SetModelSelectionChanged(uipb.ModelSelectionChanged_builder{
			Selection: mapModelSelection(selection),
		}.Build())
		return request, nil
	case domainui.FrameSessionList, domainui.FrameSessionChanged, domainui.FrameSessionInformation,
		domainui.FrameSessionTree, domainui.FrameSessionTreeNavigation, domainui.FrameSessionTreeFailed,
		domainui.FrameSessionForked, domainui.FrameSessionCloned, domainui.FrameEntryLabelSet:
		return nil, errors.New("map UI frame: session frame was not mapped")
	default:
		return nil, errors.New("map UI frame: payload is required")
	}
}

// mapSessionFrame maps lifecycle frames to protobuf payloads without losing optional fields.
func mapSessionFrame(frame domainui.Frame) (*uipb.OpenRequest, bool, error) {
	request := new(uipb.OpenRequest)
	switch frame.Kind {
	case domainui.FrameSessionList:
		mapped := lo.Map(frame.Sessions, func(value session.Summary, _ int) *uipb.SessionSummary {
			return mapSessionSummary(value)
		})
		request.SetSessionList(uipb.SessionList_builder{Sessions: mapped}.Build())
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
		request.SetSessionInformation(uipb.SessionInformation_builder{
			Info: mapSessionInfo(info), Statistics: mapSessionStatistics(statistics),
		}.Build())
		return request, true, nil
	case domainui.FrameInitialization, domainui.FrameLifecycle, domainui.FrameAuthorization,
		domainui.FrameInformation, domainui.FrameError, domainui.FrameModelSelectionChanged,
		domainui.FrameSessionTree, domainui.FrameSessionTreeNavigation, domainui.FrameSessionTreeFailed,
		domainui.FrameEntryLabelSet:
		return nil, false, nil
	default:
		return nil, false, nil
	}
}

// mapReplacementSessionFrame maps create, resume, fork, and clone replacement state.
func mapReplacementSessionFrame(
	request *uipb.OpenRequest,
	frame domainui.Frame,
) (*uipb.OpenRequest, bool, error) {
	info, present := frame.SessionInfo.Get()
	if !present {
		return nil, true, errors.New("map UI frame: session information is required")
	}
	entries, err := mapRestoredSessionEntries(frame.SessionEntries)
	if err != nil {
		return nil, true, err
	}
	changed := uipb.SessionChanged_builder{Info: mapSessionInfo(info), Entries: entries}.Build()
	if frame.Kind == domainui.FrameSessionForked {
		nextInput, nextInputPresent := frame.Text.Get()
		if !nextInputPresent {
			return nil, true, errors.New("map UI fork frame: next input is required")
		}
		request.SetSessionForked(uipb.SessionForked_builder{Session: changed, NextInput: new(nextInput)}.Build())
		return request, true, nil
	}
	if frame.Kind == domainui.FrameSessionCloned {
		request.SetSessionCloned(uipb.SessionCloned_builder{Session: changed}.Build())
		return request, true, nil
	}
	request.SetSessionChanged(changed)
	return request, true, nil
}

func mapRestoredSessionEntries(entries []domainui.SessionEntry) ([]*uipb.SessionEntry, error) {
	return lo.MapErr(entries, func(entry domainui.SessionEntry, index int) (*uipb.SessionEntry, error) {
		wire := new(uipb.SessionEntry)
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
			wire.SetToolResult(uipb.ToolResult_builder{
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
func mapRestoredUserMessage(user model.Message) (*uipb.UserMessage, error) {
	content, err := lo.MapErr(user.Content, mapRestoredUserContent)
	if err != nil {
		return nil, err
	}
	return uipb.UserMessage_builder{Content: content}.Build(), nil
}

// mapRestoredUserContent validates and maps one restored user content item.
func mapRestoredUserContent(item model.InputContent, index int) (*uipb.UserContent, error) {
	wire := new(uipb.UserContent)
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
		image := uipb.UserImage_builder{MediaType: new(mediaType), Data: nil}.Build()
		image.SetData(bytes.Clone(data))
		wire.SetImage(image)
	default:
		return nil, fmt.Errorf("map restored user content %d: unknown kind %d", index, item.Kind)
	}
	return wire, nil
}

// mapInitializationFrame validates and maps the selected initialization payload.
func mapInitializationFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	return mapRequiredFramePayload(
		frame.Initialization,
		"map UI frame: initialization payload is required",
		mapInitialization,
		(*uipb.OpenRequest).SetInitialization,
	)
}

// mapLifecycleFrame validates and maps the selected lifecycle payload.
func mapLifecycleFrame(frame domainui.Frame) (*uipb.OpenRequest, error) {
	return mapRequiredFramePayload(
		frame.Lifecycle,
		"map UI frame: lifecycle payload is required",
		mapLifecycle,
		(*uipb.OpenRequest).SetLifecycle,
	)
}

// mapRequiredFramePayload validates, maps, and installs one required frame payload.
func mapRequiredFramePayload[A, B any](
	payload mo.Option[A],
	missingMessage string,
	mapper func(A) (B, error),
	set func(*uipb.OpenRequest, B),
) (*uipb.OpenRequest, error) {
	value, present := payload.Get()
	if !present {
		return nil, errors.New(missingMessage)
	}
	mapped, err := mapper(value)
	if err != nil {
		return nil, err
	}
	request := &uipb.OpenRequest{}
	set(request, mapped)
	return request, nil
}
