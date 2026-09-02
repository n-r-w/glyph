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

	// Act: select using a value that requires shared normalization.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: " Second ", ActiveUI: mo.Some("first"),
	})

	// Assert: only the explicit candidate starts and the same connected runtime is returned.
	require.NoError(t, err)
	assert.Equal(t, "second", selection.ID)
	assert.Same(t, selectedRuntime, selection.Runtime)
	assert.Empty(t, selection.Issues)
}

// TestSelectorUsesActiveSelectionWhenExplicitIsAbsent verifies settings priority and reuse.
func TestSelectorUsesActiveSelectionWhenExplicitIsAbsent(t *testing.T) {
	t.Parallel()
	// Arrange catalog, factory, and selectedRuntime for Selector.Select to verify settings priority and reuse.

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	selectedRuntime := NewMockRuntime(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	candidate := domainui.Candidate{ID: "active-ui", Path: "/ui/active"}
	catalog.EXPECT().Discover(gomock.Any(), directory).Return(
		domainui.Discovery{Candidates: []domainui.Candidate{candidate}}, nil,
	)
	factory.EXPECT().Start(gomock.Any(), candidate).Return(selectedRuntime, nil)

	// Act by invoking Selector.Select to exercise settings priority and reuse.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.Some(" Active_UI "),
	})

	// Assert settings priority and reuse.
	require.NoError(t, err)
	assert.Equal(t, "active-ui", selection.ID)
	assert.Same(t, selectedRuntime, selection.Runtime)
}

// TestSelectorRejectsAbsentExplicitSelectionWithoutProbing verifies no fallback candidate starts.
func TestSelectorRejectsAbsentExplicitSelectionWithoutProbing(t *testing.T) {
	t.Parallel()
	// Arrange catalog, factory, and directory for Selector.Select to verify no fallback candidate starts.

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	catalog.EXPECT().Discover(gomock.Any(), directory).Return(domainui.Discovery{
		Candidates: []domainui.Candidate{{ID: "other", Path: "/ui/other"}},
	}, nil)

	// Act by invoking Selector.Select to exercise no fallback candidate starts.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "missing", ActiveUI: mo.Some("other"),
	})

	// Assert no fallback candidate starts.
	require.Error(t, err)
	assert.Nil(t, selection.Runtime)
	assert.ErrorContains(t, err, "is absent")
}

// TestSelectorDoesNotFallbackWhenExplicitStartFails verifies a selected startup failure is terminal.
func TestSelectorDoesNotFallbackWhenExplicitStartFails(t *testing.T) {
	t.Parallel()
	// Arrange catalog, factory, and directory for Selector.Select to verify a selected startup failure is terminal.

	catalog := NewMockCatalog(gomock.NewController(t))
	factory := NewMockRuntimeFactory(gomock.NewController(t))
	directory := domainui.Directory{Path: "/ui"}
	candidate := domainui.Candidate{ID: "selected", Path: "/ui/selected"}
	catalog.EXPECT().
		Discover(gomock.Any(), directory).
		Return(domainui.Discovery{Candidates: []domainui.Candidate{candidate}}, nil)
	factory.EXPECT().Start(gomock.Any(), candidate).Return(nil, errors.New("startup failed"))

	// Act by invoking Selector.Select to exercise a selected startup failure is terminal.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "selected", ActiveUI: mo.None[string](),
	})

	// Assert a selected startup failure is terminal.
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
	// Arrange catalog, factory, and directory for Selector.Select to verify automatic probe diagnostics survive failure.

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

	// Act by invoking Selector.Select to exercise automatic probe diagnostics survive failure.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.None[string](),
	})

	// Assert automatic probe diagnostics survive failure.
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
	// Arrange catalog, factory, and probeRuntime for Selector.Select to verify diagnostics survive final startup failure.

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

	// Act by invoking Selector.Select to exercise diagnostics survive final startup failure.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.None[string](),
	})

	// Assert diagnostics survive final startup failure.
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
	// Arrange catalog, factory, and firstRuntime for Selector.Select to verify automatic selection requires exactly one.

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

	// Act by invoking Selector.Select to exercise automatic selection requires exactly one.
	selection, err := NewSelector(catalog, factory).Select(t.Context(), SelectionRequest{
		Directory: directory, ExplicitUI: "", ActiveUI: mo.None[string](),
	})

	// Assert automatic selection requires exactly one.
	require.Error(t, err)
	assert.Nil(t, selection.Runtime)
	assert.ErrorContains(t, err, "multiple compatible UI plugins")
}
