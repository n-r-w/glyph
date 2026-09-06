//go:build integration

package runtime

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
	operationv1 "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestInitializationOwnsStartupAndSelectionDelivery keeps report issues unique and transfers warning delivery once.
func TestInitializationOwnsStartupAndSelectionDelivery(t *testing.T) {
	t.Parallel()
	// Arrange a selected stream with successful initialization lifecycle and one excluded candidate.
	stream := NewMockUIService_OpenClient[uiv1.OpenRequest, uiv1.OpenResponse](gomock.NewController(t))
	stream.EXPECT().Context().Return(t.Context()).AnyTimes()
	accepted := new(uiv1.UIEvent)
	accepted.SetAccepted(new(operationv1.Accepted))
	running := new(uiv1.UIEvent)
	running.SetRunning(new(operationv1.Running))
	completed := new(uiv1.UIEvent)
	completion := new(uiv1.UICompleted)
	completion.SetInitialized(new(uiv1.Initialized))
	completed.SetCompleted(completion)
	gomock.InOrder(
		stream.EXPECT().Recv().Return(uiLifecycleResponse(accepted), nil),
		stream.EXPECT().Recv().Return(uiLifecycleResponse(running), nil),
		stream.EXPECT().Recv().Return(uiLifecycleResponse(completed), nil),
	)
	var delivered *uiv1.Initialization
	stream.EXPECT().
		Send(gomock.Any()).
		DoAndReturn(func(request *uiv1.OpenRequest) error { delivered = request.GetRequest().GetInitialize(); return nil })
	var fallback strings.Builder
	service := New()
	// The generated stream replaces a process connection while the real output writer and tracker run.
	service.stream = stream
	service.BindSelection(
		hostui.Selection{
			ID: "selected",
			Issues: []hostui.SelectionIssue{
				{Candidate: hostui.Candidate{ID: "excluded", Path: "/excluded"}, Err: errors.New("probe cause")},
			},
		},
		&fallback,
	)
	issue := startup.Issue{PluginIDs: []string{"broken"}, Path: "/broken", Err: errors.New("registration cause")}
	require.NoError(t, service.ReportIssue(t.Context(), issue))
	require.NoError(
		t,
		service.ReportSummary(t.Context(), startup.LoadReport{Issues: []startup.Issue{issue}, Extensions: nil}),
	)
	// Act through public initialization and repeated output close.
	require.NoError(t, service.Initialize(t.Context(), testInitialization()))
	require.NoError(t, service.Close())
	require.NoError(t, service.Close())
	// Assert authoritative issues and selection warnings appeared once in initialization, never on stderr.
	require.NotNil(t, delivered)
	require.Len(t, delivered.GetStartupContent(), 3)
	var text strings.Builder
	for _, item := range delivered.GetStartupContent() {
		text.WriteString(item.GetText())
	}
	require.Equal(t, 1, strings.Count(text.String(), "registration cause"))
	require.Equal(t, 1, strings.Count(text.String(), "probe cause"))
	require.Empty(t, fallback.String())
}
