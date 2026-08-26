package ui

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

func TestSessionLifecycleCommandsSendTypedFrames(t *testing.T) {
	t.Parallel()

	info := testSessionInfo("stored")
	summary := session.Summary{Info: info, FirstUserText: mo.None[string](), TotalMessages: 0}
	tests := []struct {
		name          string
		command       domainui.Command
		expectedKind  domainui.FrameKind
		expectControl func(*MockSessionControl)
		assertFrame   func(*testing.T, domainui.Frame)
	}{
		{
			name: "create", command: testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
			expectedKind: domainui.FrameSessionChanged,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Create(gomock.Any()).Return(info, nil)
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) { assert.Equal(t, info, frame.SessionInfo.MustGet()) },
		},
		{
			name: "list", command: testSessionCommand(domainui.CommandListSessions, mo.None[string](), mo.None[string]()),
			expectedKind: domainui.FrameSessionList,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().List(gomock.Any()).Return([]session.Summary{summary}, nil)
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) {
				assert.Equal(t, []session.Summary{summary}, frame.Sessions)
			},
		},
		{
			name: "resume", command: testSessionCommand(domainui.CommandResumeSession, mo.Some("stored"), mo.None[string]()),
			expectedKind: domainui.FrameSessionChanged,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Resume(gomock.Any(), session.ID("stored")).Return(info, nil)
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) { assert.Equal(t, info, frame.SessionInfo.MustGet()) },
		},
		{
			name: "information", command: testSessionCommand(domainui.CommandGetSessionInfo, mo.None[string](), mo.None[string]()),
			expectedKind: domainui.FrameSessionInformation,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Info().Return(info)
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) { assert.Equal(t, info, frame.SessionInfo.MustGet()) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			control := NewMockSessionControl(controller)
			test.expectControl(control)
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				assert.Equal(t, test.expectedKind, frame.Kind)
				test.assertFrame(t, frame)
				return nil
			})
			handled, err := NewSession(channel, nil, nil, nil, control, nil).applySessionCommand(t.Context(), test.command)
			require.NoError(t, err)
			assert.True(t, handled)
		})
	}
}

func TestSessionLifecycleRejectionsSendSafeInformation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		command       domainui.Command
		expectedText  string
		expectControl func(*MockSessionControl)
	}{
		{
			name:         "resume requires id",
			command:      testSessionCommand(domainui.CommandResumeSession, mo.None[string](), mo.None[string]()),
			expectedText: "A session ID is required.", expectControl: func(*MockSessionControl) {},
		},
		{
			name:         "name requires value",
			command:      testSessionCommand(domainui.CommandSetSessionName, mo.None[string](), mo.None[string]()),
			expectedText: "A session name is required.", expectControl: func(*MockSessionControl) {},
		},
		{
			name:         "create busy",
			command:      testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
			expectedText: "Session replacement is unavailable.",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Create(gomock.Any()).Return(session.Info{}, session.ErrBusy)
			},
		},
		{
			name:         "resume not found",
			command:      testSessionCommand(domainui.CommandResumeSession, mo.Some("missing"), mo.None[string]()),
			expectedText: "Session replacement is unavailable.",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Resume(gomock.Any(), session.ID("missing")).Return(session.Info{}, os.ErrNotExist)
			},
		},
		{
			name:         "list failure",
			command:      testSessionCommand(domainui.CommandListSessions, mo.None[string](), mo.None[string]()),
			expectedText: "Sessions are unavailable.",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().List(gomock.Any()).Return(nil, errors.New("sensitive storage failure"))
			},
		},
		{
			name:         "name failure",
			command:      testSessionCommand(domainui.CommandSetSessionName, mo.None[string](), mo.Some("name")),
			expectedText: "Session naming is unavailable.",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().SetName(gomock.Any(), "name").Return(session.Info{}, errors.New("sensitive storage failure"))
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			control := NewMockSessionControl(controller)
			test.expectControl(control)
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				assert.Equal(t, domainui.FrameInformation, frame.Kind)
				assert.Equal(t, test.expectedText, frame.Text.MustGet())
				return nil
			})
			handled, err := NewSession(channel, nil, nil, nil, control, nil).applySessionCommand(t.Context(), test.command)
			require.NoError(t, err)
			assert.True(t, handled)
		})
	}
}

func TestSessionNameAndQueriesRemainAvailableDuringActiveRun(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	control := NewMockSessionControl(controller)
	info := testSessionInfo("active")
	control.EXPECT().Create(gomock.Any()).Return(session.Info{}, session.ErrBusy)
	control.EXPECT().Resume(gomock.Any(), session.ID("stored")).Return(session.Info{}, session.ErrBusy)
	control.EXPECT().SetName(gomock.Any(), "renamed").Return(info, nil)
	control.EXPECT().List(gomock.Any()).Return([]session.Summary{}, nil)
	control.EXPECT().Info().Return(info)
	channel.EXPECT().Send(gomock.Any()).Times(5)
	usecase := NewSession(channel, nil, nil, nil, control, nil)
	cancel := func() {}
	commands := []domainui.Command{
		testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
		testSessionCommand(domainui.CommandResumeSession, mo.Some("stored"), mo.None[string]()),
		testSessionCommand(domainui.CommandSetSessionName, mo.None[string](), mo.Some("renamed")),
		testSessionCommand(domainui.CommandListSessions, mo.None[string](), mo.None[string]()),
		testSessionCommand(domainui.CommandGetSessionInfo, mo.None[string](), mo.None[string]()),
	}
	for _, command := range commands {
		availability, activeCancel, activeKind, err := usecase.applyCommand(
			t.Context(), domainui.AvailabilityRunning, cancel, operationRun, command, make(chan operationResult),
		)
		require.NoError(t, err)
		assert.Equal(t, domainui.AvailabilityRunning, availability)
		assert.Equal(t, operationRun, activeKind)
		assert.NotNil(t, activeCancel)
	}
}

func testSessionCommand(kind domainui.CommandKind, id, name mo.Option[string]) domainui.Command {
	return domainui.Command{
		Kind: kind, Text: mo.None[string](), ProviderID: mo.None[string](), ModelID: mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](), SessionID: id, SessionName: name,
	}
}

func testSessionInfo(id session.ID) session.Info {
	return session.Info{
		ID: id, Name: mo.Some("named"), WorkingDirectory: "/project", StoragePath: mo.Some("/sessions/stored.jsonl"),
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}
}
