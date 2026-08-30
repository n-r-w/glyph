//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

const runtimeInterruptedSessionID = "runtime-interrupted"

type uiRuntimeFailureTransport struct {
	dataDirectory string
	effectPath    string
	releasePath   string
	requestCount  atomic.Int32
}

func (transport *uiRuntimeFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	requestNumber := transport.requestCount.Add(1)
	storagePath, err := latestRuntimeSessionPath(transport.dataDirectory)
	if err != nil {
		return nil, err
	}
	body := finalResponseSSE
	switch requestNumber {
	case 1:
		if err = os.Chmod(storagePath, 0o400); err != nil {
			return nil, err
		}
	case 2:
		command := "printf tool-effect > " + shellPath(transport.effectPath) +
			"; chmod 0400 " + shellPath(storagePath)
		body = strings.ReplaceAll(toolResponseSSE, "printf tool-ok", command)
	case 3:
		if err = waitRuntimeRelease(transport.releasePath); err != nil {
			return nil, err
		}
		if err = os.Chmod(storagePath, 0o400); err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("runtime UI transport received a dependent provider request")
	}
	return runtimeFailureHTTPResponse(body), nil
}

func runtimeFailureHTTPResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
		Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0,
		TransferEncoding: nil, Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
	}
}

func latestRuntimeSessionPath(dataDirectory string) (string, error) {
	var latestPath string
	var latestTime time.Time
	err := filepath.WalkDir(
		filepath.Join(dataDirectory, "sessions"),
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				return nil
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				return infoErr
			}
			if latestPath == "" || info.ModTime().After(latestTime) {
				latestPath = path
				latestTime = info.ModTime()
			}
			return nil
		},
	)
	if err != nil {
		return "", err
	}
	if latestPath == "" {
		return "", errors.New("runtime UI transport found no session file")
	}
	return latestPath, nil
}

func shellPath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func waitRuntimeRelease(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	var signal [1]byte
	_, err = io.ReadFull(file, signal[:])
	return err
}

func runRuntimeFailureUI(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	initialization *uipb.Initialization,
) error {
	observation := runtimeFailureProcessObservation{}
	startup := observeSessionInfo(initialization.GetSessionInfo())
	if startup.ID == "" {
		return errors.New("runtime UI helper has no startup session")
	}
	if err := configureRestartSelection(stream); err != nil {
		return err
	}

	named, err := runtimeUIName(stream, "durable runtime session")
	if err != nil {
		return err
	}
	if err = os.Chmod(named.StoragePath, 0o400); err != nil {
		return err
	}
	failureText, err := runtimeUINameFailure(stream, "private failed name")
	if err != nil {
		return err
	}
	if err = os.Chmod(named.StoragePath, 0o600); err != nil {
		return err
	}
	blockedText, err := runtimeUINameFailure(stream, "blocked after permission restore")
	if err != nil {
		return err
	}
	observation.NamingSafe = persistenceFailureText(failureText) && persistenceFailureText(blockedText)
	active, err := runtimeUIInformation(stream)
	if err != nil {
		return err
	}
	observation.IdentityPreserved = active.ID == named.ID && active.Name == named.Name
	observation.QueriesReadable = active.ID == named.ID

	created, err := runtimeUICreate(stream)
	if err != nil {
		return err
	}
	projectDirectory := filepath.Dir(named.StoragePath)
	if err = os.Chmod(projectDirectory, 0o500); err != nil {
		return err
	}
	if err = runtimeUISubmit(stream, "private first user"); err != nil {
		return err
	}
	firstEvents, firstText, err := runtimeUICollectFailure(stream, nil)
	if restoreErr := os.Chmod(projectDirectory, 0o700); err == nil {
		err = restoreErr
	}
	if err != nil {
		return err
	}
	observation.FirstUserSafe = persistenceFailureText(firstText) && slices.Equal(firstEvents, []uipb.LifecycleType{
		uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_START,
		uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_END,
		uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED,
	})
	active, err = runtimeUIInformation(stream)
	if err != nil {
		return err
	}
	observation.IdentityPreserved = observation.IdentityPreserved && active.ID == created.ID
	observation.QueriesReadable = observation.QueriesReadable && active.ID == created.ID

	if _, err = runtimeUICreate(stream); err != nil {
		return err
	}
	modelSession, err := runtimeUIName(stream, "model runtime failure")
	if err != nil {
		return err
	}
	if err = runtimeUISubmit(stream, "private model user"); err != nil {
		return err
	}
	modelEvents, modelText, err := runtimeUICollectFailure(stream, nil)
	if err != nil {
		return err
	}
	if err = os.Chmod(modelSession.StoragePath, 0o600); err != nil {
		return err
	}
	observation.ModelSafe = persistenceFailureText(modelText) &&
		!slices.Contains(modelEvents, uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END) &&
		!slices.Contains(modelEvents, uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START)

	if _, err = runtimeUICreate(stream); err != nil {
		return err
	}
	toolSession, err := runtimeUIName(stream, "tool runtime failure")
	if err != nil {
		return err
	}
	if err = runtimeUISubmit(stream, "private tool user"); err != nil {
		return err
	}
	toolEvents, toolText, err := runtimeUICollectFailure(stream, nil)
	if err != nil {
		return err
	}
	if err = os.Chmod(toolSession.StoragePath, 0o600); err != nil {
		return err
	}
	effect, effectErr := os.ReadFile(os.Getenv(appUIRuntimeEffectEnvironment))
	observation.ToolCompleted = effectErr == nil && string(effect) == "tool-effect"
	observation.ToolSafe = persistenceFailureText(toolText) &&
		slices.Contains(toolEvents, uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_START) &&
		!slices.Contains(toolEvents, uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_EXECUTION_END) &&
		!slices.Contains(toolEvents, uipb.LifecycleType_LIFECYCLE_TYPE_TOOL_RESULT)

	if _, err = runtimeUICreate(stream); err != nil {
		return err
	}
	contentionSession, err := runtimeUIName(stream, "contention runtime failure")
	if err != nil {
		return err
	}
	if err = runtimeUISubmit(stream, "private contention user"); err != nil {
		return err
	}
	contentionEvents, err := runtimeUIUntilMessageStart(stream)
	if err != nil {
		return err
	}
	busyText, err := runtimeUIResumeFailure(stream, contentionSession.ID)
	if err != nil {
		return err
	}
	observation.ContentionBusy = busyText == "Session replacement is unavailable: another operation is active"
	if err = signalRuntimeRelease(os.Getenv(appUIRuntimeReleaseEnvironment)); err != nil {
		return err
	}
	restEvents, contentionText, err := runtimeUICollectFailure(stream, contentionEvents)
	if err != nil {
		return err
	}
	if err = os.Chmod(contentionSession.StoragePath, 0o600); err != nil {
		return err
	}
	observation.ModelSafe = observation.ModelSafe && persistenceFailureText(contentionText) &&
		!slices.Contains(restEvents, uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_END)
	resumed, err := runtimeUIResumeSuccess(stream, contentionSession.ID)
	if err != nil {
		return err
	}
	observation.GateReleased = resumed.ID == contentionSession.ID

	if err = writeRuntimeInterruptedSession(stream.Context(), contentionSession); err != nil {
		return err
	}
	resumeText, err := runtimeUIResumeFailure(stream, runtimeInterruptedSessionID)
	if err != nil {
		return err
	}
	active, err = runtimeUIInformation(stream)
	if err != nil {
		return err
	}
	observation.ResumeSafe = persistenceFailureText(resumeText) && active.ID == contentionSession.ID
	interruptedPath := filepath.Join(filepath.Dir(contentionSession.StoragePath), "runtime-interrupted.jsonl")
	if err = clearImmutable(stream.Context(), interruptedPath); err != nil {
		return err
	}
	recovered, err := runtimeUIResumeSuccess(stream, runtimeInterruptedSessionID)
	if err != nil {
		return err
	}
	observation.GateReleased = observation.GateReleased && recovered.ID == runtimeInterruptedSessionID
	observation.Complete = observation.NamingSafe && observation.FirstUserSafe && observation.ModelSafe &&
		observation.ToolSafe && observation.ToolCompleted && observation.ResumeSafe && observation.ContentionBusy &&
		observation.GateReleased && observation.QueriesReadable && observation.IdentityPreserved
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err = os.WriteFile(os.Getenv(appUITraceEnvironment), encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

func runtimeUIName(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	name string,
) (sessionInfoObservation, error) {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SetSessionName field.
	if err := stream.Send(
		uipb.OpenResponse_builder{SetSessionName: uipb.SetSessionNameCommand_builder{Name: &name}.Build()}.Build(),
	); err != nil {
		return sessionInfoObservation{}, err
	}
	frame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionInformation() != nil },
	)
	if err != nil {
		return sessionInfoObservation{}, err
	}
	return observeSessionInfo(frame.GetSessionInformation().GetInfo()), nil
}

func runtimeUINameFailure(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	name string,
) (string, error) {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SetSessionName field.
	if err := stream.Send(
		uipb.OpenResponse_builder{SetSessionName: uipb.SetSessionNameCommand_builder{Name: &name}.Build()}.Build(),
	); err != nil {
		return "", err
	}
	frame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetInformation() != nil },
	)
	if err != nil {
		return "", err
	}
	return frame.GetInformation().GetText(), nil
}

func runtimeUICreate(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
) (sessionInfoObservation, error) {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active CreateSession field.
	if err := stream.Send(uipb.OpenResponse_builder{CreateSession: &uipb.CreateSessionCommand{}}.Build()); err != nil {
		return sessionInfoObservation{}, err
	}
	frame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionChanged() != nil },
	)
	if err != nil {
		return sessionInfoObservation{}, err
	}
	return observeSessionInfo(frame.GetSessionChanged().GetInfo()), nil
}

func runtimeUIInformation(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
) (sessionInfoObservation, error) {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active GetSessionInfo field.
	if err := stream.Send(
		uipb.OpenResponse_builder{GetSessionInfo: &uipb.GetSessionInfoCommand{}}.Build(),
	); err != nil {
		return sessionInfoObservation{}, err
	}
	frame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionInformation() != nil },
	)
	if err != nil {
		return sessionInfoObservation{}, err
	}
	return observeSessionInfo(frame.GetSessionInformation().GetInfo()), nil
}

func runtimeUISubmit(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse], text string) error {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Submit field.
	return stream.Send(uipb.OpenResponse_builder{Submit: uipb.SubmitCommand_builder{Text: &text}.Build()}.Build())
}

func runtimeUICollectFailure(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	initial []uipb.LifecycleType,
) ([]uipb.LifecycleType, string, error) {
	events := append([]uipb.LifecycleType(nil), initial...)
	terminalText := ""
	settled := false
	for {
		frame, err := stream.Recv()
		if err != nil {
			return nil, "", err
		}
		if hostError := frame.GetError(); hostError != nil {
			if !persistenceFailureText(hostError.GetText()) {
				return nil, "", fmt.Errorf("unsafe Host runtime error: %s", hostError.GetText())
			}
			continue
		}
		lifecycle := frame.GetLifecycle()
		if lifecycle == nil {
			continue
		}
		if lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
			if settled && lifecycle.GetAvailability() == uipb.Availability_AVAILABILITY_IDLE {
				return events, terminalText, nil
			}
			continue
		}
		events = append(events, lifecycle.GetType())
		if lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_END {
			terminalText = lifecycle.GetErrorMessage()
		}
		if lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AGENT_SETTLED {
			settled = true
		}
	}
}

func runtimeUIUntilMessageStart(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
) ([]uipb.LifecycleType, error) {
	events := make([]uipb.LifecycleType, 0)
	for {
		frame, err := stream.Recv()
		if err != nil {
			return nil, err
		}
		lifecycle := frame.GetLifecycle()
		if lifecycle == nil || lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_AVAILABILITY_CHANGED {
			continue
		}
		events = append(events, lifecycle.GetType())
		if lifecycle.GetType() == uipb.LifecycleType_LIFECYCLE_TYPE_MESSAGE_START {
			return events, nil
		}
	}
}

func runtimeUIResumeFailure(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	id string,
) (string, error) {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ResumeSession field.
	if err := stream.Send(
		uipb.OpenResponse_builder{ResumeSession: uipb.ResumeSessionCommand_builder{SessionId: &id}.Build()}.Build(),
	); err != nil {
		return "", err
	}
	frame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetInformation() != nil },
	)
	if err != nil {
		return "", err
	}
	return frame.GetInformation().GetText(), nil
}

func runtimeUIResumeSuccess(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	id string,
) (sessionInfoObservation, error) {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ResumeSession field.
	if err := stream.Send(
		uipb.OpenResponse_builder{ResumeSession: uipb.ResumeSessionCommand_builder{SessionId: &id}.Build()}.Build(),
	); err != nil {
		return sessionInfoObservation{}, err
	}
	frame, err := receiveSessionFrame(
		stream,
		func(frame *uipb.OpenRequest) bool { return frame.GetSessionChanged() != nil },
	)
	if err != nil {
		return sessionInfoObservation{}, err
	}
	return observeSessionInfo(frame.GetSessionChanged().GetInfo()), nil
}

func signalRuntimeRelease(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err = file.Write([]byte{'r'}); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func writeRuntimeInterruptedSession(ctx context.Context, active sessionInfoObservation) error {
	path := filepath.Join(filepath.Dir(active.StoragePath), "runtime-interrupted.jsonl")
	header := fmt.Sprintf(
		`{"type":"session","version":2,"id":%q,"createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n",
		runtimeInterruptedSessionID,
		active.WorkingDirectory,
	)
	user := `{"type":"entry","entry":{"type":"user","id":"runtime-preceding",` +
		`"parentId":null,"createdAt":"2026-08-27T10:00:01Z",` +
		`"message":{"content":[{"kind":1,"text":"runtime preceding"}]}}}` + "\n"
	if err := os.WriteFile(path, []byte(header+user+`{"type":"entry","entry":{"type":"model"`), 0o600); err != nil {
		return err
	}
	command := exec.CommandContext(ctx, "/usr/bin/chflags", "uchg", path)
	return command.Run()
}

func clearImmutable(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "/usr/bin/chflags", "nouchg", path).Run()
}

func persistenceFailureText(text string) bool {
	return strings.Contains(text, "session persistence failed")
}
