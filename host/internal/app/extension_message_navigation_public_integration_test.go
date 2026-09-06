//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestProgrammaticPreCommitAppendSurvivesNavigationCancellation verifies nested handler work commits independently.
//
//nolint:paralleltest // This test replaces process-global provider HTTP transport.
func TestProgrammaticPreCommitAppendSurvivesNavigationCancellation(t *testing.T) {
	// Arrange one public-only extension with a checkpoint-targeted request handler.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	directory := buildPublicExtensionFixture(t)
	var count atomic.Int32
	var body atomic.Value
	var extensionMode atomic.Value
	extensionMode.Store("session-state")
	previous := http.DefaultTransport
	http.DefaultTransport = catalogueProviderTransport(t, &count, &body, func() string {
		return extensionMode.Load().(string)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("append", "store message"))
	report := decodeSessionStateReport(t, externalToolOutput(t, body.Load().([]byte)))

	request := new(programmaticpb.OpenRequest)
	request.SetOperationId("cancel-after-append")
	payload := new(programmaticpb.ControllerRequest)
	payload.SetNavigateSessionTree(programmaticpb.NavigateSessionTree_builder{
		TargetEntryId: new(report.EntryID), SummaryMode: new(programmaticpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
		CustomFocus: nil,
	}.Build())
	request.SetRequest(payload)
	require.NoError(t, fixture.stream.Send(request))

	// Act by observing the independent append before canceled navigation completion.
	order := make([]string, 0, 2)
	var added *programmaticpb.SessionTreeEntry
	var completed *programmaticpb.SessionTreeNavigationResult
	for completed == nil {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetConnectionEvent().GetSessionEntryAdded() != nil {
			added = response.GetConnectionEvent().GetSessionEntryAdded().GetEntry()
			order = append(order, "append")
			continue
		}
		if response.GetOperationId() == "cancel-after-append" &&
			response.GetEvent().GetCompleted().GetSessionTreeNavigation() != nil {
			completed = response.GetEvent().GetCompleted().GetSessionTreeNavigation()
			order = append(order, "complete")
		}
	}

	// Assert cancellation did not roll back the pre-commit handler append.
	require.Equal(t, []string{"append", "complete"}, order)
	require.Equal(t, "request handler appended", added.GetExtensionMessage().GetText())
	require.Equal(t, programmaticpb.SessionTreeNavigationStatus_SESSION_TREE_NAVIGATION_STATUS_CANCELED,
		completed.GetStatus())
	tree := sendProgrammaticOperation(t, fixture, "tree-after-cancel", func(operation *programmaticpb.OpenRequest) {
		programmaticRequest(operation).SetGetSessionTree(new(programmaticpb.GetSessionTree))
	}).GetSessionTree().GetTree()
	require.Equal(t, added.GetId(), tree.GetActiveLeafId())
}

// TestUINavigationPublishesSnapshotBeforeObserverAppend verifies the real UI contract's ordered state delivery.
//
//nolint:paralleltest // This test replaces process-global provider HTTP transport.
func TestUINavigationPublishesSnapshotBeforeObserverAppend(t *testing.T) {
	// Arrange one real UI process and public-only extension with a navigation observer that awaits its append.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	extensions := buildPublicExtensionFixture(t)
	var count atomic.Int32
	var body atomic.Value
	previous := http.DefaultTransport
	http.DefaultTransport = catalogueProviderTransport(t, &count, &body, func() string { return "session-state" })
	t.Cleanup(func() { http.DefaultTransport = previous })
	trace := filepath.Join(t.TempDir(), "ui-extension-navigation.json")
	ready := trace + ".ready"
	gate := trace + ".gate"
	require.NoError(t, syscall.Mkfifo(ready, 0o600))
	require.NoError(t, syscall.Mkfifo(gate, 0o600))
	uiDirectory := t.TempDir()
	writeConfiguredUIExecutable(t, uiDirectory, "extension-message-ui", trace, "extension-message-navigation")
	runResult := make(chan error, 1)
	go func() {
		runResult <- runWithPaths(t.Context(), paths, cli.Command{
			Mode: cli.ModeUI, Headless: headless.Command{}, ExtensionDirectory: extensions,
			UIDirectory: uiDirectory, UIID: "extension-message-ui", SocketPath: "",
		}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	readyResult := make(chan error, 1)
	go func() {
		_, readyErr := os.ReadFile(ready)
		readyResult <- readyErr
	}()
	select {
	case err := <-readyResult:
		require.NoError(t, err)
	case err := <-runResult:
		require.NoError(t, err)
		require.FailNow(t, "UI application exited before navigation readiness")
	}
	providerCallsBeforeNavigation := count.Load()

	// Act by releasing navigation after the initial Agent Core run and waiting for UI completion.
	require.NoError(t, os.WriteFile(gate, []byte("navigate"), 0o600))
	require.NoError(t, <-runResult)

	// Assert progress, observer append, and metadata-only completion arrived in exact order.
	data, err := os.ReadFile(trace)
	require.NoError(t, err)
	var observation uiExtensionNavigationObservation
	require.NoError(t, json.Unmarshal(data, &observation))
	require.Equal(t, []string{"progress", "append", "complete"}, observation.Order)
	require.Equal(t, "observer appended", observation.AddedText)
	require.Equal(t, uiv1.ClientVisibility_CLIENT_VISIBILITY_VISIBLE.String(), observation.AddedVisibility)
	require.Equal(t, observation.ProgressActiveLeaf, observation.TerminalDestinationID)
	require.Equal(t, observation.ProgressActiveLeaf, observation.TerminalActiveLeaf)
	require.Equal(t, observation.TerminalActiveLeaf, observation.AddedParentID)
	require.NotEqual(t, observation.TerminalActiveLeaf, observation.AddedID)
	require.Equal(t, observation.AddedID, observation.ClientActiveLeaf)
	require.Equal(t, "exact\nrestart message", observation.TerminalNextInput)
	require.Equal(t, providerCallsBeforeNavigation, count.Load(), "navigation started an Agent Core request")
}

// uiExtensionNavigationObservation records public UI state transitions across one navigation.
type uiExtensionNavigationObservation struct {
	// Order contains the received state-bearing event kinds.
	Order []string `json:"order"`
	// ProgressActiveLeaf is the navigation commit leaf from operation progress.
	ProgressActiveLeaf string `json:"progress_active_leaf"`
	// AddedID identifies the later observer message.
	AddedID string `json:"added_id"`
	// AddedParentID is the observer message parent.
	AddedParentID string `json:"added_parent_id"`
	// AddedText is the exact observer message text.
	AddedText string `json:"added_text"`
	// AddedVisibility is the observer message's closed client visibility name.
	AddedVisibility string `json:"added_visibility"`
	// TerminalDestinationID is the navigation destination from metadata-only completion.
	TerminalDestinationID string `json:"terminal_destination_id"`
	// TerminalActiveLeaf is the navigation commit leaf from metadata-only completion.
	TerminalActiveLeaf string `json:"terminal_active_leaf"`
	// TerminalNextInput is the exact selected message text.
	TerminalNextInput string `json:"terminal_next_input"`
	// ClientActiveLeaf is the active leaf retained by ordered UI notification application.
	ClientActiveLeaf string `json:"client_active_leaf"`
}

// runExtensionMessageNavigationUIFixture drives the public UI SDK and records ordered navigation state.
func runExtensionMessageNavigationUIFixture(ctx context.Context, host *uisdk.Host) error {
	if err := waitForIdle(ctx, host); err != nil {
		return err
	}
	submit := new(uiv1.UIRequest)
	submit.SetSubmit(uiv1.SubmitCommand_builder{Text: new("store message")}.Build())
	submitOperation, err := host.Start(ctx, "append", submit)
	if err != nil {
		return err
	}
	var report sessionStateReportValue
	var initialAdded *uiv1.SessionTreeEntry
	idleReceived := false
	_, err = waitUIOperation(ctx, host, "append", submitOperation, func(notification *uisdk.Notification) {
		if progress := notification.Progress(); progress != nil {
			for _, content := range progress.GetAgentEvent().GetToolResultContents() {
				var candidate sessionStateReportValue
				if decodeErr := json.Unmarshal([]byte(content.GetText()), &candidate); decodeErr == nil &&
					candidate.MessageID != "" {
					report = candidate
				}
			}
		}
		if connection := notification.ConnectionEvent(); connection != nil {
			if connection.GetSessionEntryAdded() != nil {
				initialAdded = connection.GetSessionEntryAdded().GetEntry()
			}
			if connection.GetAvailabilityChanged().GetAvailability() == uiv1.Availability_AVAILABILITY_IDLE {
				idleReceived = true
			}
		}
	})
	if err != nil {
		return err
	}
	if report.MessageID == "" || report.MessageParentID == "" {
		return errors.New("initial extension message report was not received")
	}
	if initialAdded == nil || initialAdded.GetExtensionMessage() == nil ||
		initialAdded.GetExtensionMessage().GetText() != report.MessageText ||
		initialAdded.GetExtensionMessage().GetVisibility() != uiv1.ClientVisibility_CLIENT_VISIBILITY_HIDDEN {
		return errors.New("initial extension message connection event is invalid")
	}
	if !idleReceived {
		if err := waitForIdle(ctx, host); err != nil {
			return err
		}
	}
	trace := os.Getenv(appUITraceEnvironment)
	if err := os.WriteFile(trace+".ready", []byte("ready"), 0o600); err != nil {
		return err
	}
	if _, err := os.ReadFile(trace + ".gate"); err != nil {
		return err
	}

	navigation := new(uiv1.UIRequest)
	navigation.SetNavigateSessionTree(uiv1.NavigateSessionTreeCommand_builder{
		TargetEntryId: new(report.MessageID), SummaryMode: new(uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
		CustomFocus: nil,
	}.Build())
	navigationOperation, err := host.Start(ctx, "navigate-message", navigation)
	if err != nil {
		return err
	}
	observation := uiExtensionNavigationObservation{
		Order: nil, ProgressActiveLeaf: "", AddedID: "", AddedParentID: "", AddedText: "",
		AddedVisibility: "", TerminalDestinationID: "", TerminalActiveLeaf: "",
		TerminalNextInput: "", ClientActiveLeaf: "",
	}
	var observationErr error
	result, err := waitUIOperation(ctx, host, "navigate-message", navigationOperation,
		func(notification *uisdk.Notification) {
			switch notification.Kind() {
			case uisdk.NotificationProgress:
				value := notification.Progress().GetSessionTreeNavigation()
				if value == nil || observation.ProgressActiveLeaf != "" {
					return
				}
				observation.Order = append(observation.Order, "progress")
				observation.ProgressActiveLeaf = value.GetTree().GetActiveLeafId()
				observation.ClientActiveLeaf = observation.ProgressActiveLeaf
			case uisdk.NotificationConnectionEvent:
				added := notification.ConnectionEvent().GetSessionEntryAdded().GetEntry()
				if added == nil || added.GetExtensionMessage() == nil {
					observationErr = errors.New("observer append connection event was not received")
					return
				}
				observation.Order = append(observation.Order, "append")
				observation.AddedID = added.GetId()
				observation.AddedParentID = added.GetParentId()
				observation.AddedText = added.GetExtensionMessage().GetText()
				observation.AddedVisibility = added.GetExtensionMessage().GetVisibility().String()
				observation.ClientActiveLeaf = added.GetId()
			case uisdk.NotificationCompleted:
				terminal := notification.Completed().GetSessionTreeNavigation()
				if terminal == nil {
					observationErr = errors.New("navigation terminal metadata was not received")
					return
				}
				observation.Order = append(observation.Order, "complete")
				observation.TerminalDestinationID = terminal.GetDestinationId()
				observation.TerminalActiveLeaf = terminal.GetActiveLeafId()
				observation.TerminalNextInput = terminal.GetNextInput()
			case uisdk.NotificationFailed:
				observationErr = notification.OperationError()
			}
		})
	if err != nil {
		return err
	}
	if observationErr != nil {
		return observationErr
	}
	if result.GetSessionTreeNavigation() == nil {
		return errors.New("navigation operation result was not received")
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		return fmt.Errorf("encode UI extension navigation observation: %w", err)
	}
	if err := os.WriteFile(trace, encoded, 0o600); err != nil {
		return err
	}
	return host.Close(ctx)
}

// TestProgrammaticNavigationPublishesSnapshotBeforeObserverAppend verifies ordered progress, append,
// and terminal delivery.
//
//nolint:paralleltest // This test replaces process-global provider HTTP transport.
func TestProgrammaticNavigationPublishesSnapshotBeforeObserverAppend(t *testing.T) {
	// Arrange one public-only extension that stores a selectable message and appends from its navigation observer.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	directory := buildPublicExtensionFixture(t)
	var count atomic.Int32
	var body atomic.Value
	var extensionMode atomic.Value
	extensionMode.Store("session-state")
	previous := http.DefaultTransport
	http.DefaultTransport = catalogueProviderTransport(t, &count, &body, func() string {
		return extensionMode.Load().(string)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("append", "store message"))
	report := decodeSessionStateReport(t, externalToolOutput(t, body.Load().([]byte)))
	providerCallsBeforeNavigation := count.Load()
	require.NotEmpty(t, report.MessageID)
	require.NotEmpty(t, report.MessageParentID)

	request := new(programmaticpb.OpenRequest)
	request.SetOperationId("navigate-message")
	payload := new(programmaticpb.ControllerRequest)
	payload.SetNavigateSessionTree(programmaticpb.NavigateSessionTree_builder{
		TargetEntryId: new(report.MessageID),
		SummaryMode:   new(programmaticpb.SummaryMode_SUMMARY_MODE_NO_SUMMARY), CustomFocus: nil,
	}.Build())
	request.SetRequest(payload)
	require.NoError(t, fixture.stream.Send(request))

	// Act by recording the three state-bearing frames from the ordered Programmatic writer.
	order := make([]string, 0, 3)
	var progress *programmaticpb.SessionTreeNavigationProgress
	var added *programmaticpb.SessionTreeEntry
	var completed *programmaticpb.SessionTreeNavigationResult
	for completed == nil {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		if response.GetOperationId() == "navigate-message" &&
			response.GetEvent().GetProgress().GetSessionTreeNavigation() != nil {
			progress = response.GetEvent().GetProgress().GetSessionTreeNavigation()
			order = append(order, "progress")
			continue
		}
		if response.GetConnectionEvent().GetSessionEntryAdded() != nil {
			require.Empty(t, response.GetOperationId())
			added = response.GetConnectionEvent().GetSessionEntryAdded().GetEntry()
			order = append(order, "append")
			continue
		}
		if response.GetOperationId() == "navigate-message" &&
			response.GetEvent().GetCompleted().GetSessionTreeNavigation() != nil {
			completed = response.GetEvent().GetCompleted().GetSessionTreeNavigation()
			order = append(order, "complete")
		}
	}

	// Assert the appended state follows progress and survives metadata-only completion.
	require.Equal(t, []string{"progress", "append", "complete"}, order)
	require.NotNil(t, progress)
	require.Equal(t, report.MessageParentID, progress.GetTree().GetActiveLeafId())
	require.NotNil(t, added)
	require.Equal(t, "observer appended", added.GetExtensionMessage().GetText())
	require.Equal(t, report.MessageParentID, added.GetParentId())
	require.Equal(t, report.MessageParentID, completed.GetDestinationId())
	require.Equal(t, report.MessageParentID, completed.GetActiveLeafId())
	require.Equal(t, "exact\nrestart message", completed.GetNextInput())

	tree := sendProgrammaticOperation(t, fixture, "tree-after-observer", func(operation *programmaticpb.OpenRequest) {
		programmaticRequest(operation).SetGetSessionTree(new(programmaticpb.GetSessionTree))
	}).GetSessionTree().GetTree()
	require.Equal(t, added.GetId(), tree.GetActiveLeafId())
	require.Equal(t, programmaticpb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE,
		added.GetExtensionMessage().GetVisibility())
	require.Equal(t, providerCallsBeforeNavigation, count.Load(), "navigation started an Agent Core request")
	require.NotEmpty(t, bytes.TrimSpace(body.Load().([]byte)))
}
