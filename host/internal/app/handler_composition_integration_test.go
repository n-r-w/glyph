//go:build integration

package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// handlerFixtureModeEnvironment selects the child-process handler set.
	handlerFixtureModeEnvironment = "GLYPH_HANDLER_FIXTURE_MODE"
	// handlerFixtureObserverEnvironment provides the observer output path.
	handlerFixtureObserverEnvironment = "GLYPH_HANDLER_FIXTURE_OBSERVER"
	// handlerFixtureSupplyMode selects the request-result supplying handler.
	handlerFixtureSupplyMode = "supply"
	// handlerFixtureRefineMode selects the result refiner and observer handlers.
	handlerFixtureRefineMode = "refine"
)

// grpcHandlerFixture provides ordered request, result, and observer behavior from a child process.
type grpcHandlerFixture struct {
	// mode selects the registered handler set.
	mode string
	// observerPath records post-commit observation.
	observerPath string
}

// handlerFixtureRegisterOperation returns the fixture registration.
type handlerFixtureRegisterOperation struct {
	// fixture owns the configured handler set.
	fixture *grpcHandlerFixture
}

// handlerFixtureHandleOperation invokes one configured fixture handler.
type handlerFixtureHandleOperation struct {
	// fixture owns the configured handler behavior.
	fixture *grpcHandlerFixture
	// request contains the typed handler invocation.
	request *extensionpb.HandleRequest
}

// TestSessionTreeComposesRealGRPCHandlers verifies two extension processes compose before and after one commit.
func TestSessionTreeComposesRealGRPCHandlers(t *testing.T) {
	t.Parallel()

	// Arrange: two ordered real extension processes and one summarized navigation.
	extensionDirectory := t.TempDir()
	writeHandlerFixtureScript(t, extensionDirectory, "01-supply", handlerFixtureSupplyMode, "")
	observerPath := filepath.Join(t.TempDir(), "observed")
	writeHandlerFixtureScript(t, extensionDirectory, "02-refine", handlerFixtureRefineMode, observerPath)
	extensions := extensionservice.New(catalog.New(), extensionruntime.NewFactory(), func(
		context.Context,
		tool.RuntimeFailure,
	) error {
		return nil
	})
	t.Cleanup(extensions.Close)
	report, err := extensions.Load(t.Context(), extensionservice.Directory{Path: extensionDirectory, Explicit: true})
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	extensions.Activate(t.Context())

	controller := gomock.NewController(t)
	active := sessiontree.NewMockActiveSession(controller)
	models := sessiontree.NewMockModelRequester(controller)
	tree := grpcNavigationTree(t)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	active.EXPECT().Tree().Return(tree)
	active.EXPECT().SessionID().Return("session")
	models.EXPECT().ActiveSelection().Return(selection)
	models.EXPECT().CheckAvailability(gomock.Any(), selection).Return(nil)
	committed := tree.Clone()
	require.NoError(t, committed.SetActiveLeaf(mo.Some("root")))
	require.NoError(t, committed.Add(grpcSummaryEntry(selection)))
	active.EXPECT().CommitNavigation(gomock.Any(), sessiontree.CommitCommand{
		ExpectedActiveLeafID: mo.Some("active"), DestinationID: mo.Some("root"),
		BranchSummary: mo.Some(sessiontree.BranchSummaryDraft{
			Summary: "refined", FirstEntryID: "user", LastEntryID: "active",
			CommonAncestorID: mo.Some("root"), Selection: selection, Usage: mo.None[session.TokenUsage](),
		}),
	}).Return(committed, nil)
	service := sessiontree.New(active, models, extensions)

	// Act: through request supply, result refinement, atomic commit, and observer delivery.
	result, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
		TargetEntryID: "user", SummaryMode: sessionnavigation.SummaryModeSummarize,
		CustomFocus: mo.None[string](),
	})

	// Assert: the refined result committed before the real observer recorded the event.
	require.NoError(t, err)
	assert.False(t, result.Canceled)
	assert.Empty(t, result.Issues)
	observed, err := os.ReadFile(observerPath)
	require.NoError(t, err)
	assert.Equal(t, "session:refined", string(observed))
}

// TestGRPCHandlerFixture runs one extension server only inside a child process.
func TestGRPCHandlerFixture(t *testing.T) {
	t.Parallel()

	// Arrange: read the child-process handler mode and observer path.
	mode := os.Getenv(handlerFixtureModeEnvironment)
	if mode == "" {
		return
	}
	// Act: serve the valid handler fixture through generated SDK mocks.
	fixture := &grpcHandlerFixture{
		mode: mode, observerPath: os.Getenv(handlerFixtureObserverEnvironment),
	}
	extensionsdk.Serve(newHandlerMockService(t, fixture))

	// Assert: go-plugin owns fixture lifetime after Serve starts.
}

// newHandlerMockService creates generated SDK mocks for one valid handler fixture.
func newHandlerMockService(t *testing.T, fixture *grpcHandlerFixture) extensionsdk.Service {
	t.Helper()
	controller := gomock.NewController(t)
	service := extensionsdk.NewMockService(controller)
	registration := extensionsdk.NewMockRegisterOperation(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil).AnyTimes()
	registration.EXPECT().Run(gomock.Any()).DoAndReturn(
		func(context.Context) (*extensionpb.RegisterResponse, error) {
			return (&handlerFixtureRegisterOperation{fixture: fixture}).Run(t.Context())
		},
	).AnyTimes()
	registration.EXPECT().Release().AnyTimes()
	service.EXPECT().PrepareHandle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request *extensionpb.HandleRequest) (extensionsdk.HandleOperation, error) {
			handler := extensionsdk.NewMockHandleOperation(controller)
			prepared := &handlerFixtureHandleOperation{fixture: fixture, request: request}
			handler.EXPECT().Run(gomock.Any()).DoAndReturn(prepared.Run)
			handler.EXPECT().Release()
			return handler, nil
		},
	).AnyTimes()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(
		nil,
		extensionsdk.Reject("INVALID_ARGUMENT", fmt.Errorf("handler fixture registers no tools")),
	).AnyTimes()
	return service
}

// Run returns the ordered handler set owned by this fixture process.
func (operation *handlerFixtureRegisterOperation) Run(
	context.Context,
) (*extensionpb.RegisterResponse, error) {
	fixture := operation.fixture
	var handlers []*extensionpb.HandlerDescriptor
	switch fixture.mode {
	case handlerFixtureSupplyMode:
		handlers = []*extensionpb.HandlerDescriptor{
			extensionpb.HandlerDescriptor_builder{
				Id: new("supply"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_REQUEST),
			}.Build(),
		}
	case handlerFixtureRefineMode:
		handlers = []*extensionpb.HandlerDescriptor{
			extensionpb.HandlerDescriptor_builder{
				Id: new("refine"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_BEFORE_TREE_RESULT),
			}.Build(),
			extensionpb.HandlerDescriptor_builder{
				Id: new("observe"), Kind: new(extensionpb.HandlerKind_HANDLER_KIND_SESSION_TREE),
			}.Build(),
		}
	default:
		return nil, fmt.Errorf("unknown handler fixture mode %q", fixture.mode)
	}
	return extensionpb.RegisterResponse_builder{Tools: nil, Handlers: handlers}.Build(), nil
}

// Release has no fixture registration reservation to free.
func (operation *handlerFixtureRegisterOperation) Release() {}

// Run returns the action owned by this fixture handler.
func (operation *handlerFixtureHandleOperation) Run(
	_ context.Context,
) (*extensionpb.HandleResponse, error) {
	request := operation.request
	s := operation.fixture
	switch request.GetHandlerId() {
	case "supply":
		return extensionpb.HandleResponse_builder{
			SessionBeforeTreeRequest: extensionpb.SessionBeforeTreeRequestAction_builder{
				Cancel: new(false), RequestAction: new(extensionpb.RequestAction_REQUEST_ACTION_PRESERVE),
				Request: nil, ResultAction: new(extensionpb.ResultAction_RESULT_ACTION_REPLACE),
				Result: extensionpb.BranchSummaryResult_builder{Summary: new("ready"), Usage: nil}.Build(),
			}.Build(),
			SessionBeforeTreeResult: nil, SessionTree: nil, Error: nil,
		}.Build(), nil
	case "refine":
		return extensionpb.HandleResponse_builder{
			SessionBeforeTreeRequest: nil,
			SessionBeforeTreeResult: extensionpb.SessionBeforeTreeResultAction_builder{
				Cancel: new(false), ResultAction: new(extensionpb.ResultAction_RESULT_ACTION_REPLACE),
				Result: extensionpb.BranchSummaryResult_builder{Summary: new("refined"), Usage: nil}.Build(),
			}.Build(),
			SessionTree: nil, Error: nil,
		}.Build(), nil
	case "observe":
		invocation := request.GetSessionTree()
		if invocation == nil || invocation.GetCreatedSummary() == nil {
			return nil, fmt.Errorf("observer received no committed summary")
		}
		value := invocation.GetSessionId() + ":" + invocation.GetCreatedSummary().GetSummary()
		if err := os.WriteFile(s.observerPath, []byte(value), 0o600); err != nil {
			return nil, err
		}
		return extensionpb.HandleResponse_builder{
			SessionBeforeTreeRequest: nil, SessionBeforeTreeResult: nil,
			SessionTree: extensionpb.SessionTreeAction_builder{}.Build(), Error: nil,
		}.Build(), nil
	default:
		return nil, fmt.Errorf("unknown handler %q", request.GetHandlerId())
	}
}

// Release has no fixture handler reservation to free.
func (operation *handlerFixtureHandleOperation) Release() {}

// writeHandlerFixtureScript creates one catalog executable that starts the child test process.
func writeHandlerFixtureScript(t *testing.T, directory, name, mode, observerPath string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=%q %s=%q exec %q -test.run=^TestGRPCHandlerFixture$\n",
		handlerFixtureModeEnvironment,
		mode,
		handlerFixtureObserverEnvironment,
		observerPath,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(script), 0o700))
}

// grpcNavigationTree creates one abandoned user path for real handler composition.
func grpcNavigationTree(t *testing.T) session.Tree {
	t.Helper()
	createdAt := time.Unix(1, 0).UTC()
	entries := []session.Entry{
		grpcUserEntry("root", mo.None[string](), "root", createdAt),
		grpcUserEntry("user", mo.Some("root"), "user", createdAt.Add(time.Second)),
		grpcUserEntry("active", mo.Some("user"), "active", createdAt.Add(2*time.Second)),
	}
	tree, err := session.NewTree(entries, mo.Some("active"), nil)
	require.NoError(t, err)
	return tree
}

// grpcUserEntry creates one valid user entry for the integration tree.
func grpcUserEntry(id string, parentID mo.Option[string], text string, createdAt time.Time) session.Entry {
	return session.Entry{
		ID: id, ParentID: parentID, CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage(text)),
		Model: mo.None[model.Response](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[agent.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
}

// grpcSummaryEntry creates the committed summary returned by the active-session mock.
func grpcSummaryEntry(selection model.Selection) session.Entry {
	return session.Entry{
		ID: "summary", ParentID: mo.Some("root"), CreatedAt: time.Unix(4, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[model.Message](),
		Model: mo.None[model.Response](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[agent.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.Some(session.BranchSummaryEntry{
			Summary: "refined", FirstEntryID: "user", LastEntryID: "active",
			Provider: selection.Provider, Model: selection.Model, ReasoningChoice: selection.ReasoningChoice,
			Usage: mo.None[session.TokenUsage](), EstimatedCost: mo.None[session.EstimatedCost](),
		}),
	}
}
