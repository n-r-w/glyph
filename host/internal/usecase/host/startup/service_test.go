//go:build !integration

package startup

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// TestServiceLoadAppliesValidationInApprovedOrder verifies local tools, handlers, then global conflicts.
func TestServiceLoadAppliesValidationInApprovedOrder(t *testing.T) {
	t.Parallel()
	// Arrange pending registrations with one failure at each policy stage.
	controller := gomock.NewController(t)
	runtimes := NewMockRuntimeLoader(controller)
	tools := NewMockToolRegistrar(controller)
	handlers := NewMockHandlerRegistrar(controller)
	pending := PendingLoad{Issues: nil, Registrations: []PendingRegistration{
		{ID: "local-invalid", Path: "/local", Tools: nil, Handlers: nil},
		{ID: "handler-invalid", Path: "/handler", Tools: nil, Handlers: nil},
		{ID: "first", Path: "/first", Tools: nil, Handlers: nil},
		{ID: "second", Path: "/second", Tools: nil, Handlers: nil},
		{ID: "safe", Path: "/safe", Tools: nil, Handlers: nil},
	}}
	runtimes.EXPECT().LoadPending(t.Context(), Directory{Path: "/plugins", Explicit: true}).Return(pending, nil)
	localErr := errors.New("local invalid")
	handlerErr := errors.New("handler invalid")
	gomock.InOrder(
		tools.EXPECT().ValidateLocal(pending.Registrations[0]).Return(nil, localErr),
		tools.EXPECT().ValidateLocal(pending.Registrations[1]).Return([]tool.Descriptor{}, nil),
		tools.EXPECT().
			ValidateLocal(pending.Registrations[2]).
			Return([]tool.Descriptor{{Name: "shared", Description: "Shared.", InputSchemaJSON: nil, ConstrainedSampling: mo.None[tool.ConstrainedSampling]()}}, nil),
		tools.EXPECT().
			ValidateLocal(pending.Registrations[3]).
			Return([]tool.Descriptor{{Name: "shared", Description: "Shared.", InputSchemaJSON: nil, ConstrainedSampling: mo.None[tool.ConstrainedSampling]()}}, nil),
		tools.EXPECT().
			ValidateLocal(pending.Registrations[4]).
			Return([]tool.Descriptor{{Name: "safe", Description: "Safe.", InputSchemaJSON: nil, ConstrainedSampling: mo.None[tool.ConstrainedSampling]()}}, nil),
		handlers.EXPECT().ValidateHandlers(pending.Registrations[1]).Return(nil, handlerErr),
		handlers.EXPECT().ValidateHandlers(pending.Registrations[2]).Return([]AcceptedHandler{}, nil),
		handlers.EXPECT().ValidateHandlers(pending.Registrations[3]).Return([]AcceptedHandler{}, nil),
		handlers.EXPECT().ValidateHandlers(pending.Registrations[4]).Return([]AcceptedHandler{}, nil),
	)
	conflict := Issue{PluginIDs: []string{"first", "second"}, Path: "", Err: errors.New(`tool name "shared" conflicts`)}
	tools.EXPECT().Conflicts(gomock.Any()).Return([]Issue{conflict})
	runtimes.EXPECT().RejectPending([]string{"first", "handler-invalid", "local-invalid", "second"})
	accepted := []AcceptedRegistration{
		{
			ID:   "safe",
			Path: "/safe",
			Tools: []tool.Descriptor{
				{
					Name:                "safe",
					Description:         "Safe.",
					InputSchemaJSON:     nil,
					ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
				},
			},
			Handlers: []AcceptedHandler{},
		},
	}
	tools.EXPECT().Commit(accepted)
	handlers.EXPECT().CommitHandlers(accepted)
	runtimes.EXPECT().Accept(accepted)
	service := New(runtimes, tools, handlers)
	// Act load the explicit extension directory.
	report, err := service.Load(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: "/plugins"})
	// Assert only the fully accepted extension is published and errors keep their text.
	require.NoError(t, err)
	assert.Equal(t, accepted, report.Extensions)
	require.Len(t, report.Issues, 3)
	assert.ErrorContains(t, report.Issues[0].Err, "tool name")
	assert.ErrorContains(t, report.Issues[1].Err, "handler invalid")
	assert.ErrorContains(t, report.Issues[2].Err, "validate extension registration: local invalid")
}

// TestServiceLoadWrapsRuntimeLoadFailure verifies complete load errors remain in the chain.
func TestServiceLoadWrapsRuntimeLoadFailure(t *testing.T) {
	t.Parallel()
	// Arrange a runtime loader failure.
	controller := gomock.NewController(t)
	runtimes := NewMockRuntimeLoader(controller)
	loadErr := errors.New("catalog failed")
	runtimes.EXPECT().
		LoadPending(t.Context(), Directory{Path: "/data/plugins/extension", Explicit: false}).
		Return(PendingLoad{}, loadErr)
	service := New(runtimes, NewMockToolRegistrar(controller), NewMockHandlerRegistrar(controller))
	// Act load the default directory.
	_, err := service.Load(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: ""})
	// Assert every context and the original cause remain available.
	require.ErrorIs(t, err, loadErr)
	require.EqualError(t, err, "load extensions: catalog failed")
}

// TestServiceStartReportsIssuesAndSummary verifies delivery order and errors.
func TestServiceStartReportsIssuesAndSummary(t *testing.T) {
	t.Parallel()
	// Arrange one pending-load issue and no registrations.
	controller := gomock.NewController(t)
	runtimes := NewMockRuntimeLoader(controller)
	tools := NewMockToolRegistrar(controller)
	handlers := NewMockHandlerRegistrar(controller)
	reporter := NewMockReporter(controller)
	issue := Issue{PluginIDs: []string{"broken"}, Path: "/broken", Err: errors.New("failed")}
	runtimes.EXPECT().
		LoadPending(gomock.Any(), gomock.Any()).
		Return(PendingLoad{Issues: []Issue{issue}, Registrations: nil}, nil)
	tools.EXPECT().Conflicts([]AcceptedRegistration{}).Return(nil)
	runtimes.EXPECT().RejectPending([]string{})
	tools.EXPECT().Commit([]AcceptedRegistration{})
	handlers.EXPECT().CommitHandlers([]AcceptedRegistration{})
	runtimes.EXPECT().Accept([]AcceptedRegistration{})
	reporter.EXPECT().ReportIssue(t.Context(), issue).Return(nil)
	reporter.EXPECT().
		ReportSummary(t.Context(), LoadReport{Issues: []Issue{issue}, Extensions: []AcceptedRegistration{}}).
		Return(nil)
	service := New(runtimes, tools, handlers)
	// Act start and report extension state.
	report, err := service.Start(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: ""}, reporter)
	// Assert issue and summary delivery succeeds.
	require.NoError(t, err)
	assert.Equal(t, []Issue{issue}, report.Issues)
}
