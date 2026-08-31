//go:build !integration

package ui

import (
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSelectorUsesExplicitSelectionWithoutFallback verifies priority and connection reuse.
func TestSelectorUsesExplicitSelectionWithoutFallback(t *testing.T) {
	t.Parallel()

	// Arrange: expose two candidates while explicitly selecting the second.
	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	selectedRuntime := NewMockRuntime(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	candidates := []domainui.Candidate{{ID: "first", Path: "/ui/first"}, {ID: "second", Path: "/ui/second"}}
	catalog.EXPECT().Discover(gomock.Any(), directory).Return(domainui.Discovery{Candidates: candidates}, nil)
	factory.EXPECT().Start(gomock.Any(), candidates[1]).Return(selectedRuntime, nil)
	selectedRuntime.EXPECT().Capabilities().Return(domainui.Capabilities{ControlsTerminal: true})

	// Act: select using a value that requires shared normalization.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: " Second ", ActiveUI: mo.Some("first"),
	})

	// Assert: only the explicit candidate starts and the same connected runtime is returned.
	require.NoError(t, err)
	assert.Equal(t, "second", selection.ID)
	assert.Same(t, selectedRuntime, selection.Runtime)
	assert.True(t, selection.Capabilities.ControlsTerminal)
	assert.Empty(t, selection.Issues)
}

// TestSelectorUsesActiveSelectionWhenExplicitIsAbsent verifies settings priority and reuse.
func TestSelectorUsesActiveSelectionWhenExplicitIsAbsent(t *testing.T) {
	t.Parallel()

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	selectedRuntime := NewMockRuntime(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	candidate := domainui.Candidate{ID: "active-ui", Path: "/ui/active"}
	catalog.EXPECT().Discover(gomock.Any(), directory).Return(
		domainui.Discovery{Candidates: []domainui.Candidate{candidate}}, nil,
	)
	factory.EXPECT().Start(gomock.Any(), candidate).Return(selectedRuntime, nil)
	selectedRuntime.EXPECT().Capabilities().Return(domainui.Capabilities{ControlsTerminal: false})

	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.Some(" Active_UI "),
	})

	require.NoError(t, err)
	assert.Equal(t, "active-ui", selection.ID)
	assert.Same(t, selectedRuntime, selection.Runtime)
}

// TestSelectorRejectsAbsentExplicitSelectionWithoutProbing verifies no fallback candidate starts.
func TestSelectorRejectsAbsentExplicitSelectionWithoutProbing(t *testing.T) {
	t.Parallel()

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	catalog.EXPECT().Discover(gomock.Any(), directory).Return(domainui.Discovery{
		Candidates: []domainui.Candidate{{ID: "other", Path: "/ui/other"}},
	}, nil)

	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "missing", ActiveUI: mo.Some("other"),
	})

	require.Error(t, err)
	assert.Nil(t, selection.Runtime)
	assert.ErrorContains(t, err, "is absent")
}

// TestSelectorDoesNotFallbackWhenExplicitStartFails verifies a selected startup failure is terminal.
func TestSelectorDoesNotFallbackWhenExplicitStartFails(t *testing.T) {
	t.Parallel()

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	candidate := domainui.Candidate{ID: "selected", Path: "/ui/selected"}
	catalog.EXPECT().
		Discover(gomock.Any(), directory).
		Return(domainui.Discovery{Candidates: []domainui.Candidate{candidate}}, nil)
	factory.EXPECT().Start(gomock.Any(), candidate).Return(nil, errors.New("startup failed"))

	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "selected", ActiveUI: mo.None[string](),
	})

	require.Error(t, err)
	assert.Nil(t, selection.Runtime)
	assert.ErrorContains(t, err, "start selected UI")
}

// TestSelectorProbesEveryCandidateAndRestartsSoleCompatible verifies cleanup and automatic selection.
func TestSelectorProbesEveryCandidateAndRestartsSoleCompatible(t *testing.T) {
	t.Parallel()

	// Arrange: one probe succeeds, one fails, and the successful candidate has a selected restart.
	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	probeRuntime := NewMockRuntime(gomock.NewController(t))
	selectedRuntime := NewMockRuntime(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	first := domainui.Candidate{ID: "first", Path: "/ui/first"}
	second := domainui.Candidate{ID: "second", Path: "/ui/second"}
	catalog.EXPECT().
		Discover(gomock.Any(), directory).
		Return(domainui.Discovery{Candidates: []domainui.Candidate{first, second}}, nil)
	gomock.InOrder(
		factory.EXPECT().Start(gomock.Any(), first).Return(probeRuntime, nil),
		probeRuntime.EXPECT().Close(),
		factory.EXPECT().Start(gomock.Any(), second).Return(nil, errors.New("incompatible")),
		factory.EXPECT().Start(gomock.Any(), first).Return(selectedRuntime, nil),
		selectedRuntime.EXPECT().Capabilities().Return(domainui.Capabilities{ControlsTerminal: false}),
	)

	// Act: select without explicit or active preference.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.None[string](),
	})

	// Assert: all probes completed, the successful probe was stopped, and its candidate restarted once.
	require.NoError(t, err)
	assert.Equal(t, "first", selection.ID)
	assert.Same(t, selectedRuntime, selection.Runtime)
	require.Len(t, selection.Issues, 1)
	assert.Equal(t, second, selection.Issues[0].Candidate)
}

// TestSelectorReportsEveryExcludedCandidateWhenNoCompatible verifies automatic probe diagnostics survive failure.
func TestSelectorReportsEveryExcludedCandidateWhenNoCompatible(t *testing.T) {
	t.Parallel()

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	first := domainui.Candidate{ID: "first", Path: "/ui/first"}
	second := domainui.Candidate{ID: "second", Path: "/ui/second"}
	catalog.EXPECT().Discover(gomock.Any(), directory).Return(
		domainui.Discovery{Candidates: []domainui.Candidate{first, second}}, nil,
	)
	factory.EXPECT().Start(gomock.Any(), first).Return(nil, errors.New("first unavailable"))
	factory.EXPECT().Start(gomock.Any(), second).Return(nil, errors.New("second incompatible"))

	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.None[string](),
	})

	require.Error(t, err)
	assert.Nil(t, selection.Runtime)
	assert.Equal(t, []SelectionIssue{
		{Candidate: first, Err: errors.New("first unavailable")},
		{Candidate: second, Err: errors.New("second incompatible")},
	}, selection.Issues)
	require.ErrorContains(t, err, "no compatible UI plugin is available")
}

// TestSelectorRetainsProbeIssuesWhenSelectedRestartFails verifies diagnostics survive final startup failure.
func TestSelectorRetainsProbeIssuesWhenSelectedRestartFails(t *testing.T) {
	t.Parallel()

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	probeRuntime := NewMockRuntime(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	first := domainui.Candidate{ID: "first", Path: "/ui/first"}
	second := domainui.Candidate{ID: "second", Path: "/ui/second"}
	catalog.EXPECT().Discover(gomock.Any(), directory).Return(
		domainui.Discovery{Candidates: []domainui.Candidate{first, second}}, nil,
	)
	gomock.InOrder(
		factory.EXPECT().Start(gomock.Any(), first).Return(probeRuntime, nil),
		probeRuntime.EXPECT().Close(),
		factory.EXPECT().Start(gomock.Any(), second).Return(nil, errors.New("second incompatible")),
		factory.EXPECT().Start(gomock.Any(), first).Return(nil, errors.New("restart failed")),
	)

	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.None[string](),
	})

	require.Error(t, err)
	assert.Nil(t, selection.Runtime)
	assert.Equal(t, []SelectionIssue{
		{Candidate: second, Err: errors.New("second incompatible")},
		{Candidate: first, Err: errors.New("restart failed")},
	}, selection.Issues)
	require.ErrorContains(t, err, "restart automatically selected UI")
}

// TestSelectorRejectsMultipleCompatibleCandidates verifies automatic selection requires exactly one.
func TestSelectorRejectsMultipleCompatibleCandidates(t *testing.T) {
	t.Parallel()

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	firstRuntime := NewMockRuntime(gomock.NewController(t))
	secondRuntime := NewMockRuntime(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	first := domainui.Candidate{ID: "first", Path: "/ui/first"}
	second := domainui.Candidate{ID: "second", Path: "/ui/second"}
	catalog.EXPECT().
		Discover(gomock.Any(), directory).
		Return(domainui.Discovery{Candidates: []domainui.Candidate{first, second}}, nil)
	gomock.InOrder(
		factory.EXPECT().Start(gomock.Any(), first).Return(firstRuntime, nil),
		firstRuntime.EXPECT().Close(),
		factory.EXPECT().Start(gomock.Any(), second).Return(secondRuntime, nil),
		secondRuntime.EXPECT().Close(),
	)

	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.None[string](),
	})

	require.Error(t, err)
	assert.Nil(t, selection.Runtime)
	assert.ErrorContains(t, err, "multiple compatible UI plugins")
}
