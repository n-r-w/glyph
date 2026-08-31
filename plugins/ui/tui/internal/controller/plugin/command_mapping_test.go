//go:build !integration

package plugin

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestOpenMapsCommandsThroughOneStreamSender verifies UI commands use one serialized sender.
func TestOpenMapsCommandsThroughOneStreamSender(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	factory := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)
	runDone := make(chan struct{})
	emitter := make(chan Emit, 1)

	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(&bytes.Buffer{})
	factory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ presentationdomain.Event, _ io.Reader, _ io.Writer, emit Emit) Program {
			emitter <- emit
			return program
		},
	)
	program.EXPECT().Run().DoAndReturn(func() error { <-runDone; return nil })
	program.EXPECT().Quit().Do(func() { close(runDone) })
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	emit := <-emitter

	commands := []presentationdomain.Command{
		{
			Kind:            presentationdomain.CommandSubmit,
			Text:            mo.Some("hello"),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind:            presentationdomain.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind:            presentationdomain.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind:            presentationdomain.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TreeCommand:     mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind: presentationdomain.CommandCreateSession, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.None[string](),
			TreeCommand: mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind: presentationdomain.CommandListSessions, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.None[string](),
			TreeCommand: mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind: presentationdomain.CommandResumeSession, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.Some("stored"), SessionName: mo.None[string](),
			TreeCommand: mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind: presentationdomain.CommandSetSessionName, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.Some("named"),
			TreeCommand: mo.None[presentationdomain.TreeCommand](),
		},
		{
			Kind: presentationdomain.CommandGetSessionInfo, Text: mo.None[string](),
			ProviderID: mo.None[string](), ModelID: mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](), SessionName: mo.None[string](),
			TreeCommand: mo.None[presentationdomain.TreeCommand](),
		},
	}
	for _, command := range commands {
		emitResult := make(chan error, 1)
		go func() { emitResult <- emit(command) }()
		response, receiveErr := stream.Recv()
		require.NoError(t, receiveErr)
		require.NoError(t, <-emitResult)
		switch command.Kind {
		case presentationdomain.CommandSubmit:
			assert.Equal(t, "hello", response.GetSubmit().GetText())
		case presentationdomain.CommandStop:
			assert.NotNil(t, response.GetStop())
		case presentationdomain.CommandRetryAuthentication:
			assert.NotNil(t, response.GetRetryAuthentication())
		case presentationdomain.CommandQuit:
			assert.NotNil(t, response.GetQuit())
		case presentationdomain.CommandCreateSession:
			assert.NotNil(t, response.GetCreateSession())
		case presentationdomain.CommandListSessions:
			assert.NotNil(t, response.GetListSessions())
		case presentationdomain.CommandResumeSession:
			assert.Equal(t, "stored", response.GetResumeSession().GetSessionId())
		case presentationdomain.CommandSetSessionName:
			assert.Equal(t, "named", response.GetSetSessionName().GetName())
		case presentationdomain.CommandGetSessionInfo:
			assert.NotNil(t, response.GetGetSessionInfo())
		case presentationdomain.CommandUnspecified,
			presentationdomain.CommandSelectModel,
			presentationdomain.CommandSelectReasoningChoice,
			presentationdomain.CommandGetSessionTree,
			presentationdomain.CommandNavigateSessionTree,
			presentationdomain.CommandForkSession,
			presentationdomain.CommandCloneSession,
			presentationdomain.CommandSetEntryLabel:
			t.Fatal("unexpected command")
		}
	}

	require.NoError(t, stream.CloseSend())
	_, err = stream.Recv()
	assert.ErrorIs(t, err, io.EOF)
}

// TestOpenReturnsProgramErrorAndClosesTerminal verifies program failures restore terminal ownership.
func TestOpenReturnsProgramErrorAndClosesTerminal(t *testing.T) {
	t.Parallel()

	mockController := gomock.NewController(t)
	terminal := NewMockTerminal(mockController)
	session := NewMockTerminalSession(mockController)
	factory := NewMockProgramFactory(mockController)
	program := NewMockProgram(mockController)

	terminal.EXPECT().Open().Return(session, nil)
	session.EXPECT().Input().Return(bytes.NewBuffer(nil))
	session.EXPECT().Output().Return(&bytes.Buffer{})
	factory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(program)
	program.EXPECT().Run().Return(errors.New("program failed"))
	session.EXPECT().Close().Return(nil)

	client := uisdk.TestClient(t, New(terminal, factory))
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	require.NoError(t, stream.Send(initializationRequest()))
	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// TestMapInitializationPreservesWarningAndExtensionPath verifies startup diagnostics reach presentation.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()

	// Arrange initialization with a warning and an extension filesystem path.
	initialization := uiv1.Initialization_builder{
		SelectedUiId: new("glyph-tui"),
		StartupContent: []*uiv1.StartupContent{uiv1.StartupContent_builder{
			Severity: new(uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING),
			Text:     new("excluded optional UI"),
		}.Build()},
		Extensions: []*uiv1.ExtensionAvailability{uiv1.ExtensionAvailability_builder{
			PluginId: new("glyph-tools"),
			Tools:    []string{"read"},
			Path:     new("/plugins/glyph-tools"),
		}.Build()},
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE),
		Models: []*uiv1.ConfiguredModel{uiv1.ConfiguredModel_builder{
			ProviderId: new("openai-codex"),
			ModelId:    new("gpt"),
			Reasoning:  testUIReasoning(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build()},
		ModelSelection: uiv1.ModelSelection_builder{
			ProviderId:      new("openai-codex"),
			ModelId:         new("gpt"),
			ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
		}.Build(),
		SessionInfo: testSessionInfo(),
	}.Build()

	// Act by mapping the initialization payload.
	event, err := mapInitialization(initialization)

	// Assert warning severity and extension path survive mapping.
	require.NoError(t, err)
	assert.Equal(t, []presentationdomain.Line{{
		Kind:     presentationdomain.LineWarning,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Text:     mo.Some("excluded optional UI"),
		Contents: mo.None[[]presentationdomain.Content](),
	}}, event.Startup)
	assert.Equal(t, []presentationdomain.Extension{{
		ID:    "glyph-tools",
		Path:  "/plugins/glyph-tools",
		Tools: []string{"read"},
	}}, event.Extensions)
}
