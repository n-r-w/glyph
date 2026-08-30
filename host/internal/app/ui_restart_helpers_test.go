//go:build integration

package app

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// nameRecoveryStartupSession persists one empty session before the parent writes recovery fixtures.
func nameRecoveryStartupSession(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	tracePath string,
	startupInfo *uipb.SessionInfo,
) error {
	startup := observeSessionInfo(startupInfo)
	name := "recovery active"
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SetSessionName field.
	if err := stream.Send(uipb.OpenResponse_builder{SetSessionName: uipb.SetSessionNameCommand_builder{
		Name: &name,
	}.Build()}.Build()); err != nil {
		return err
	}
	frame, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionInformation() != nil
	})
	if err != nil {
		return err
	}
	named := observeSessionInfo(frame.GetSessionInformation().GetInfo())
	if named.ID != startup.ID || named.Name != name || !named.StoragePathPresent {
		return errors.New("Host did not persist the recovery startup session")
	}
	encoded, err := json.Marshal(sessionRestartObservation{
		NamedSession:       named,
		NewStartup:         sessionInfoObservation{},
		RuntimeFailureText: "",
		Complete:           false,
	})
	if err != nil {
		return err
	}
	if err = os.WriteFile(tracePath, encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

// nameStartupSession persists the first Host startup session for the restart fixture.
func nameStartupSession(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	tracePath string,
	startupInfo *uipb.SessionInfo,
) error {
	startup := observeSessionInfo(startupInfo)
	if startup.ID == "" || startup.NamePresent || startup.StoragePathPresent {
		return errors.New("first Host did not initialize an unpersisted session")
	}
	if err := configureRestartSelection(stream); err != nil {
		return err
	}
	name := "restart session"
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SetSessionName field.
	if err := stream.Send(
		uipb.OpenResponse_builder{SetSessionName: uipb.SetSessionNameCommand_builder{Name: &name}.Build()}.Build(),
	); err != nil {
		return err
	}
	frame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionInformation() != nil },
	)
	if err != nil {
		return err
	}
	named := observeSessionInfo(frame.GetSessionInformation().GetInfo())
	if named.ID != startup.ID || !named.NamePresent || named.Name != name || !named.StoragePathPresent {
		return errors.New("Host did not persist the startup session name")
	}
	statistics := frame.GetSessionInformation().GetStatistics()
	if statistics.GetTotalMessages() != 0 || !statistics.HasTokens() || statistics.GetTokens().GetTotalTokens() != 0 {
		return errors.New("named empty session did not expose available zero token totals")
	}
	if err = submitRestartTurn(stream, "restart text"); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active GetSessionInfo field.
	if err = stream.Send(uipb.OpenResponse_builder{GetSessionInfo: &uipb.GetSessionInfoCommand{}}.Build()); err != nil {
		return err
	}
	frame, err = receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionInformation() != nil
	})
	if err != nil {
		return err
	}
	named = observeSessionInfo(frame.GetSessionInformation().GetInfo())
	statistics = frame.GetSessionInformation().GetStatistics()
	if statistics.GetTotalMessages() != 4 || statistics.HasTokens() {
		return errors.New("completed session did not expose counts with unavailable token totals")
	}
	encoded, err := json.Marshal(sessionRestartObservation{
		NamedSession:       named,
		NewStartup:         sessionInfoObservation{},
		RuntimeFailureText: "",
		Complete:           false,
	})
	if err != nil {
		return err
	}
	if err = os.WriteFile(tracePath, encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

func assertUIStatistics(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	totalMessages int64,
	tokensAvailable bool,
	totalTokens int64,
) error {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active GetSessionInfo field.
	if err := stream.Send(
		uipb.OpenResponse_builder{GetSessionInfo: &uipb.GetSessionInfoCommand{}}.Build(),
	); err != nil {
		return err
	}
	frame, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionInformation() != nil
	})
	if err != nil {
		return err
	}
	statistics := frame.GetSessionInformation().GetStatistics()
	if statistics.GetTotalMessages() != totalMessages || statistics.HasTokens() != tokensAvailable {
		return errors.New("session statistics availability or count did not match")
	}
	if tokensAvailable && statistics.GetTokens().GetTotalTokens() != totalTokens {
		return errors.New("session token total did not match")
	}
	return nil
}

// configureRestartSelection commits one model selection before restart behavior runs.
func configureRestartSelection(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
) error {
	providerID := "openai-codex"
	modelID := "selected-model"
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SelectModel field.
	if err := stream.Send(uipb.OpenResponse_builder{SelectModel: uipb.SelectModelCommand_builder{
		ProviderId: &providerID, ModelId: &modelID,
	}.Build()}.Build()); err != nil {
		return err
	}
	frame, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetModelSelectionChanged() != nil
	})
	if err != nil {
		return err
	}
	selection := frame.GetModelSelectionChanged().GetSelection()
	if selection.GetProviderId() != providerID || selection.GetModelId() != modelID {
		return errors.New("Host did not commit the selected provider and model")
	}
	high := uipb.ReasoningChoice_REASONING_CHOICE_HIGH
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SelectReasoningChoice field.
	if err = stream.Send(uipb.OpenResponse_builder{SelectReasoningChoice: uipb.SelectReasoningChoiceCommand_builder{
		Choice: &high,
	}.Build()}.Build()); err != nil {
		return err
	}
	frame, err = receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetModelSelectionChanged() != nil
	})
	if err != nil {
		return err
	}
	selection = frame.GetModelSelectionChanged().GetSelection()
	if selection.GetProviderId() != providerID || selection.GetModelId() != modelID ||
		selection.GetReasoningChoice() != high {
		return errors.New("Host did not commit the selected reasoning choice")
	}
	return nil
}

func submitRestartTurn(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	text string,
) error {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Submit field.
	if err := stream.Send(
		uipb.OpenResponse_builder{Submit: uipb.SubmitCommand_builder{Text: &text}.Build()}.Build(),
	); err != nil {
		return err
	}
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if hostError := frame.GetError(); hostError != nil {
			return fmt.Errorf("restart turn failed: %s", hostError.GetText())
		}
		lifecycle := frame.GetLifecycle()
		if lifecycle != nil && lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED {
			return nil
		}
	}
}

// receiveSessionFrame rejects replay content while waiting for one lifecycle response.
func receiveSessionFrame(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	matches func(*uipb.OpenRequest) bool,
) (*uipb.OpenRequest, error) {
	for {
		frame, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		if matches(frame) {
			return frame, nil
		}
		if lifecycle := frame.GetLifecycle(); lifecycle != nil {
			if lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
				continue
			}
			return nil, fmt.Errorf("unexpected transcript lifecycle frame: %s", lifecycle.GetType())
		}
		if hostError := frame.GetError(); hostError != nil {
			return nil, fmt.Errorf("session lifecycle command failed: %s", hostError.GetText())
		}
		return nil, errors.New("unexpected Host UI frame during session lifecycle")
	}
}

// observeSessionInfo captures every mapped scalar and its explicit presence state.
func observeSessionInfo(info *uipb.SessionInfo) sessionInfoObservation {
	observation := sessionInfoObservation{}
	if info == nil {
		return observation
	}
	createdTime := ""
	if info.GetCreatedTime() != nil {
		createdTime = info.GetCreatedTime().AsTime().Format(time.RFC3339Nano)
	}
	updateTime := ""
	if info.GetUpdateTime() != nil {
		updateTime = info.GetUpdateTime().AsTime().Format(time.RFC3339Nano)
	}
	return sessionInfoObservation{
		ID: info.GetId(), Name: info.GetName(), WorkingDirectory: info.GetWorkingDirectory(),
		StoragePath: info.GetStoragePath(), CreatedTime: createdTime, UpdateTime: updateTime,
		IDPresent: info.HasId(), NamePresent: info.HasName(), WorkingDirectoryPresent: info.HasWorkingDirectory(),
		StoragePathPresent: info.HasStoragePath(), CreatedTimePresent: info.HasCreatedTime(),
		UpdateTimePresent: info.HasUpdateTime(),
	}
}
