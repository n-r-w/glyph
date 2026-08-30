package ui

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSetSessionNamePreservesTranscriptFrameKind verifies naming returns information with current statistics.
func TestSetSessionNamePreservesTranscriptFrameKind(t *testing.T) {
	t.Parallel()

	// Arrange a successful name change and current empty-session statistics.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	control := NewMockSessionControl(controller)
	info := session.Info{}
	control.EXPECT().SetName(gomock.Any(), "renamed").Return(info, nil)
	control.EXPECT().Information().Return(session.InformationSnapshot{
		Info: info,
		Statistics: session.Statistics{
			UserMessages:   0,
			ModelResponses: 0,
			ToolCalls:      0,
			ToolResults:    0,
			TotalMessages:  0,
			TokenUsage: mo.Some(
				session.TokenUsage{},
			),
			EstimatedCost: mo.None[session.EstimatedCost](),
			CostBreakdown: nil,
		},
	})
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		assert.Equal(t, domainui.FrameSessionInformation, frame.Kind)
		assert.Equal(t, info, frame.SessionInfo.MustGet())
		return nil
	})
	usecase := NewSession(channel, nil, nil, nil, control, nil)

	// Act by applying the name command.
	handled, err := usecase.applySessionCommand(t.Context(), domainui.Command{
		Kind:            domainui.CommandSetSessionName,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName: mo.Some(
			"renamed",
		),
		TargetEntryID: mo.None[string](),
		SummaryMode:   domainui.SummaryModeNoSummary,
		CustomFocus:   mo.None[string](),
		EntryLabel:    mo.None[string](),
	})
	// Assert the command is handled and the information frame is sent.
	require.NoError(t, err)
	assert.True(t, handled)
}

// sessionContextKey isolates context-propagation evidence within this test package.
type sessionContextKey struct{}

// testUICommand creates a complete command for focused session lifecycle tests.
func testUICommand(kind domainui.CommandKind, text mo.Option[string]) domainui.Command {
	return domainui.Command{
		Kind:            kind,
		Text:            text,
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       mo.None[string](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     domainui.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
}

// SessionSuite shares channel, runner, and authenticator mocks across lifecycle scenarios.
type SessionSuite struct {
	suite.Suite
	channel       *MockChannel
	runner        *MockAgentRunner
	authenticator *MockAuthenticator
	modelCatalog  *MockModelCatalog
}

// SetupTest creates isolated session boundaries for each lifecycle scenario.
func (s *SessionSuite) SetupTest() {
	controller := gomock.NewController(s.T())
	s.channel = NewMockChannel(controller)
	s.channel.EXPECT().Close().AnyTimes()
	s.runner = NewMockAgentRunner(controller)
	s.authenticator = NewMockAuthenticator(controller)
	s.modelCatalog = NewMockModelCatalog(controller)
}

// TestSessionInitializationDeliveryFailureSkipsActivation starts no Host work before initialization succeeds.
func (s *SessionSuite) TestSessionInitializationDeliveryFailureSkipsActivation() {
	t := s.T()

	s.channel.EXPECT().Send(gomock.Any()).Return(io.ErrClosedPipe)
	activated := false

	err := NewSession(s.channel, s.runner, s.authenticator, s.modelCatalog, nil, func(context.Context) {
		activated = true
	}).Run(t.Context(), domainui.Initialization{
		SelectedUIID:   "test-ui",
		StartupContent: nil,
		Extensions:     nil,
		Availability:   domainui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(domainui.ModelSelection{}),
		SessionInfo:    session.Info{},
	})

	require.ErrorIs(t, err, io.ErrClosedPipe)
	assert.False(t, activated)
}

// containsRetryableError reports whether one retryable error frame matches text.
func containsRetryableError(frames []domainui.Frame, text string) bool {
	return lo.ContainsBy(frames, func(frame domainui.Frame) bool {
		return frame.Kind == domainui.FrameError && frame.RetryAuthentication.MustGet() && frame.Text.MustGet() == text
	})
}

// containsInformation reports whether one information frame contains text.
func containsInformation(frames []domainui.Frame, text string) bool {
	return lo.ContainsBy(frames, func(frame domainui.Frame) bool {
		return frame.Kind == domainui.FrameInformation && strings.Contains(frame.Text.MustGet(), text)
	})
}

// containsAvailability reports whether one lifecycle frame carries availability.
func containsAvailability(frames []domainui.Frame, availability domainui.Availability) bool {
	return lo.ContainsBy(frames, func(frame domainui.Frame) bool {
		return frame.Kind == domainui.FrameLifecycle && frame.Lifecycle.MustGet().Availability.MustGet() == availability
	})
}
