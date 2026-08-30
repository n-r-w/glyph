package tui

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestFactoryEmitsSubmittedTerminalInput verifies Bubble Tea input reaches the plugin stream.
func TestFactoryEmitsSubmittedTerminalInput(t *testing.T) {
	t.Parallel()

	// Arrange a Bubble Tea program with pipe input and captured commands.
	service := presentationusecase.New()
	factory := NewFactory(service.Apply)
	input, writeInput := io.Pipe()
	t.Cleanup(func() { _ = writeInput.Close() })
	output := newNotifyingWriter()
	emitted := make(chan presentationdomain.Command, 1)
	program := factory.New(
		testEvent(testEventPayload{
			Kind:                 presentationdomain.EventInitialization,
			Availability:         mo.Some(presentationdomain.AvailabilityIdle),
			Position:             mo.None[int](),
			Text:                 mo.None[string](),
			ModelResponseContent: nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		}),
		input, output,
		func(command presentationdomain.Command) error {
			emitted <- command
			return nil
		},
	)

	// Act by starting the program and writing submitted Unicode input.
	runResult := make(chan error, 1)
	go func() { runResult <- program.Run() }()
	select {
	case <-output.written:
	case <-t.Context().Done():
		t.Fatal("Bubble Tea did not start")
	}
	_, err := io.WriteString(writeInput, "héllo🙂\r")
	require.NoError(t, err)

	// Assert the program emits the complete submit command and exits cleanly.
	select {
	case command := <-emitted:
		assert.Equal(t, presentationdomain.Command{
			Kind:            presentationdomain.CommandSubmit,
			Text:            mo.Some("héllo🙂"),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[presentationdomain.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
		}, command)
	case <-t.Context().Done():
		t.Fatal("Bubble Tea did not emit submitted input")
	}
	program.Quit()
	require.NoError(t, <-runResult)
}

// TestFactoryRunsProgramWithSuppliedTerminalIO verifies the selected terminal files are used.
func TestFactoryRunsProgramWithSuppliedTerminalIO(t *testing.T) {
	t.Parallel()

	// Arrange a program with caller-supplied input and notifying output.
	service := presentationusecase.New()
	factory := NewFactory(service.Apply)
	output := newNotifyingWriter()
	program := factory.New(
		testEvent(testEventPayload{
			Kind:                 presentationdomain.EventInitialization,
			Availability:         mo.Some(presentationdomain.AvailabilityIdle),
			Position:             mo.None[int](),
			Text:                 mo.None[string](),
			ModelResponseContent: nil,
			ModelSelection:       mo.None[presentationdomain.ModelSelection](),
			SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		}),
		bytes.NewBuffer(nil), output,
		func(presentationdomain.Command) error { return nil },
	)

	// Act by running the program, sending an event, and requesting shutdown.
	runResult := make(chan error, 1)
	go func() { runResult <- program.Run() }()

	select {
	case <-output.written:
	case <-t.Context().Done():
		t.Fatal("Bubble Tea did not write to the supplied terminal output")
	}
	program.Send(testEvent(testEventPayload{
		Kind:                 presentationdomain.EventInformation,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		Text:                 mo.Some("stream event"),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
	}))
	program.Quit()

	// Assert shutdown succeeds and the supplied output receives rendered content.
	require.NoError(t, <-runResult)
	assert.NotEmpty(t, output.String())
}

// notifyingWriter records output and signals the first completed render.
type notifyingWriter struct {
	mutex   sync.Mutex
	buffer  bytes.Buffer
	written chan struct{}
}

// newNotifyingWriter creates an output recorder with a first-write signal.
func newNotifyingWriter() *notifyingWriter {
	return &notifyingWriter{
		written: make(chan struct{}, 1),
		mutex:   sync.Mutex{},
		buffer:  bytes.Buffer{},
	}
}

// Write records rendered bytes and closes the first-write signal once.
func (writer *notifyingWriter) Write(content []byte) (int, error) {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	select {
	case writer.written <- struct{}{}:
	default:
	}
	return writer.buffer.Write(content)
}

// String returns a stable copy of rendered bytes.
func (writer *notifyingWriter) String() string {
	writer.mutex.Lock()
	defer writer.mutex.Unlock()
	return writer.buffer.String()
}
