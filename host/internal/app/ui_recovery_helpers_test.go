//go:build integration

package app

import (
	"bytes"

	"encoding/json/v2"
	"errors"
	"fmt"

	"os"

	"strings"

	"github.com/samber/lo"

	"google.golang.org/grpc"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

type sessionInfoObservation struct {
	ID                      string `json:"id"`
	Name                    string `json:"name"`
	WorkingDirectory        string `json:"working_directory"`
	StoragePath             string `json:"storage_path"`
	CreatedTime             string `json:"created_time"`
	UpdateTime              string `json:"update_time"`
	IDPresent               bool   `json:"id_present"`
	NamePresent             bool   `json:"name_present"`
	WorkingDirectoryPresent bool   `json:"working_directory_present"`
	StoragePathPresent      bool   `json:"storage_path_present"`
	CreatedTimePresent      bool   `json:"created_time_present"`
	UpdateTimePresent       bool   `json:"update_time_present"`
}

type sessionRestartObservation struct {
	NamedSession       sessionInfoObservation `json:"named_session"`
	NewStartup         sessionInfoObservation `json:"new_startup"`
	RuntimeFailureText string                 `json:"runtime_failure_text,omitempty"`
	Complete           bool                   `json:"complete"`
}

// runSessionRestartUI verifies one persisted session across two independent Host constructions.
func runSessionRestartUI(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	initialization *uipb.Initialization,
) error {
	tracePath := os.Getenv(appUITraceEnvironment)
	payload, readErr := os.ReadFile(tracePath)
	if errors.Is(readErr, os.ErrNotExist) {
		return nameStartupSession(stream, tracePath, initialization.GetSessionInfo())
	}
	if readErr != nil {
		return readErr
	}
	var observation sessionRestartObservation
	if err := json.Unmarshal(payload, &observation); err != nil {
		return err
	}
	startup := observeSessionInfo(initialization.GetSessionInfo())
	if startup.ID == "" || startup.ID == observation.NamedSession.ID || startup.NamePresent ||
		startup.StoragePathPresent {
		return errors.New("second Host did not initialize a new unpersisted session")
	}
	observation.NewStartup = startup
	if err := configureRestartSelection(stream); err != nil {
		return err
	}
	if err := assertUIStatistics(stream, 0, true, 0); err != nil {
		return fmt.Errorf("verify restarted empty-session statistics: %w", err)
	}

	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ListSessions field.
	if err := stream.Send(uipb.OpenResponse_builder{ListSessions: &uipb.ListSessionsCommand{}}.Build()); err != nil {
		return err
	}
	listedFrame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionList() != nil },
	)
	if err != nil {
		return err
	}
	listed := listedFrame.GetSessionList().GetSessions()
	if len(listed) != 1 {
		return errors.New("stored session list did not preserve the named session")
	}
	listedInfo := observeSessionInfo(listed[0].GetInfo())
	if listedInfo.ID != observation.NamedSession.ID || listedInfo.Name != observation.NamedSession.Name ||
		listedInfo.WorkingDirectory != observation.NamedSession.WorkingDirectory ||
		listedInfo.StoragePath != observation.NamedSession.StoragePath ||
		listedInfo.CreatedTime != observation.NamedSession.CreatedTime ||
		!listedInfo.IDPresent || !listedInfo.NamePresent || !listedInfo.WorkingDirectoryPresent ||
		!listedInfo.StoragePathPresent || !listedInfo.CreatedTimePresent || !listedInfo.UpdateTimePresent {
		return errors.New("stored session list did not preserve session identity and presence state")
	}
	observation.NamedSession = listedInfo
	if !listed[0].HasFirstUserText() || listed[0].GetFirstUserText() != "restart text" ||
		listed[0].GetTotalMessages() != 7 {
		return errors.New("stored session summary did not include the full-content turn")
	}

	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ResumeSession field.
	if err = stream.Send(uipb.OpenResponse_builder{ResumeSession: uipb.ResumeSessionCommand_builder{
		SessionId: new(observation.NamedSession.ID),
	}.Build()}.Build()); err != nil {
		return err
	}
	changedFrame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionChanged() != nil },
	)
	if err != nil {
		return err
	}
	if observeSessionInfo(changedFrame.GetSessionChanged().GetInfo()) != observation.NamedSession {
		return errors.New("resumed session did not preserve every session information field and presence state")
	}
	entries := changedFrame.GetSessionChanged().GetEntries()
	if len(entries) != 7 || entries[0].GetUser() == nil || entries[1].GetModel() == nil ||
		entries[2].GetToolResult() == nil || entries[3].GetModel() == nil || entries[4].GetUser() == nil ||
		entries[5].GetModel() == nil || entries[6].GetToolResult() == nil ||
		len(entries[0].GetUser().GetContent()) != 1 || len(entries[1].GetModel().GetContent()) != 2 ||
		entries[0].GetUser().GetContent()[0].GetText() != "restart text" ||
		entries[1].GetModel().GetResponseId() != "resp-1" ||
		entries[1].GetModel().GetContent()[1].GetToolCall().GetCallId() != "call-1" ||
		entries[2].GetToolResult().GetCallId() != "call-1" ||
		!strings.Contains(entries[2].GetToolResult().GetContents()[0].GetText(), "tool-ok") ||
		entries[3].GetModel().GetContent()[0].GetText() != "Request complete." ||
		len(entries[4].GetUser().GetContent()) != 3 ||
		entries[4].GetUser().GetContent()[0].GetText() != "full user" ||
		!bytes.Equal(entries[4].GetUser().GetContent()[1].GetImage().GetData(), []byte{1, 2, 3, 4}) ||
		len(entries[5].GetModel().GetContent()) != 3 ||
		entries[5].GetModel().GetContent()[0].GetText() != "full reasoning" ||
		entries[5].GetModel().GetContent()[1].GetText() != "full refusal" ||
		entries[5].GetModel().GetContent()[2].GetToolCall().GetCallId() != "full-call" ||
		len(entries[5].GetModel().GetDiagnostics()) != 1 ||
		entries[5].GetModel().GetDiagnostics()[0].GetCode() != "full_notice" ||
		entries[5].GetModel().GetDiagnostics()[0].GetMessage() != "full diagnostic" ||
		len(entries[6].GetToolResult().GetContents()) != 2 ||
		!bytes.Equal(entries[6].GetToolResult().GetContents()[1].GetImage().GetData(), []byte{9, 8, 7, 6}) {
		return errors.New("resumed session did not restore ordered full content")
	}
	if err = assertUIStatistics(stream, 7, false, 0); err != nil {
		return fmt.Errorf("verify resumed session statistics: %w", err)
	}
	if err = submitRestartTurn(stream, "continue"); err != nil {
		return err
	}

	// A second information query proves that no replay lifecycle frame was inserted after replacement.
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active GetSessionInfo field.
	if err = stream.Send(uipb.OpenResponse_builder{GetSessionInfo: &uipb.GetSessionInfoCommand{}}.Build()); err != nil {
		return err
	}
	infoFrame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionInformation() != nil },
	)
	if err != nil {
		return err
	}
	active := observeSessionInfo(infoFrame.GetSessionInformation().GetInfo())
	if active.ID != observation.NamedSession.ID || active.Name != observation.NamedSession.Name ||
		active.WorkingDirectory != observation.NamedSession.WorkingDirectory ||
		active.StoragePath != observation.NamedSession.StoragePath || active.CreatedTime != observation.NamedSession.CreatedTime {
		return errors.New("active session identity changed after resumed continuation")
	}
	observation.NamedSession = active
	observation.Complete = true
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err = os.WriteFile(tracePath, encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

// runSessionRecoveryUI rejects completed corruption and restores the prefix before one interrupted tail.
func runSessionRecoveryUI(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	initialization *uipb.Initialization,
) error {
	tracePath := os.Getenv(appUITraceEnvironment)
	payload, readErr := os.ReadFile(tracePath)
	if errors.Is(readErr, os.ErrNotExist) {
		return nameRecoveryStartupSession(stream, tracePath, initialization.GetSessionInfo())
	}
	if readErr != nil {
		return readErr
	}
	var observation sessionRestartObservation
	if err := json.Unmarshal(payload, &observation); err != nil {
		return err
	}
	startup := observeSessionInfo(initialization.GetSessionInfo())
	for _, id := range []string{malformedRecoveryID, wrongCWDRecoveryID, unsupportedRecoveryID} {
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ResumeSession field.
		if err := stream.Send(uipb.OpenResponse_builder{ResumeSession: uipb.ResumeSessionCommand_builder{
			SessionId: new(id),
		}.Build()}.Build()); err != nil {
			return err
		}
		rejected, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
			return frame.GetInformation() != nil
		})
		if err != nil {
			return err
		}
		if !strings.HasPrefix(rejected.GetInformation().GetText(), "Session replacement is unavailable: ") {
			return errors.New("invalid resume did not return detailed unavailable information")
		}
		//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active GetSessionInfo field.
		if err = stream.Send(
			uipb.OpenResponse_builder{GetSessionInfo: &uipb.GetSessionInfoCommand{}}.Build(),
		); err != nil {
			return err
		}
		information, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
			return frame.GetSessionInformation() != nil
		})
		if err != nil {
			return err
		}
		if information.GetSessionInformation().GetInfo().GetId() != startup.ID {
			return errors.New("invalid resume replaced the previous active session")
		}
	}

	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ListSessions field.
	if err := stream.Send(uipb.OpenResponse_builder{ListSessions: &uipb.ListSessionsCommand{}}.Build()); err != nil {
		return err
	}
	listed, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionList() != nil },
	)
	if err != nil {
		return err
	}
	listedIDs := lo.Map(listed.GetSessionList().GetSessions(), func(summary *uipb.SessionSummary, _ int) string {
		return summary.GetInfo().GetId()
	})
	if len(listedIDs) != 2 || !lo.Contains(listedIDs, observation.NamedSession.ID) ||
		!lo.Contains(listedIDs, interruptedRecoveryID) {
		return errors.New("session list did not skip invalid recovery fixtures")
	}

	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ResumeSession field.
	if err = stream.Send(uipb.OpenResponse_builder{ResumeSession: uipb.ResumeSessionCommand_builder{
		SessionId: new(interruptedRecoveryID),
	}.Build()}.Build()); err != nil {
		return err
	}
	changed, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionChanged() != nil || frame.GetInformation() != nil
	})
	if err != nil {
		return err
	}
	if information := changed.GetInformation(); information != nil {
		return completeRuntimeRecoveryFailure(stream, tracePath, observation, startup, information.GetText())
	}
	entries := changed.GetSessionChanged().GetEntries()
	if changed.GetSessionChanged().GetInfo().GetId() != interruptedRecoveryID || len(entries) != 1 {
		return errors.New("interrupted-tail resume did not restore only preceding entries")
	}
	user := entries[0].GetUser()
	if user == nil || len(user.GetContent()) != 1 || user.GetContent()[0].GetText() != "preceding tail text" {
		return errors.New("interrupted-tail resume did not restore the preceding user entry")
	}
	observation.NewStartup = startup
	observation.Complete = true
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err = os.WriteFile(tracePath, encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

func completeRuntimeRecoveryFailure(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	tracePath string,
	observation sessionRestartObservation,
	startup sessionInfoObservation,
	failureText string,
) error {
	if !strings.Contains(failureText, "session persistence failed") {
		return fmt.Errorf("runtime recovery failure text: %s", failureText)
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active GetSessionInfo field.
	if err := stream.Send(
		uipb.OpenResponse_builder{GetSessionInfo: &uipb.GetSessionInfoCommand{}}.Build(),
	); err != nil {
		return err
	}
	active, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionInformation() != nil
	})
	if err != nil {
		return err
	}
	if active.GetSessionInformation().GetInfo().GetId() != startup.ID {
		return errors.New("runtime recovery failure replaced the previous active session")
	}
	observation.NewStartup = startup
	observation.RuntimeFailureText = failureText
	observation.Complete = true
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err = os.WriteFile(tracePath, encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}
