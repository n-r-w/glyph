//go:build !integration

package presentation

import (
	"fmt"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// TestDisplaySnapshotIsDetached preserves prior display data across application transitions and nested-data mutation.
func TestDisplaySnapshotIsDetached(t *testing.T) {
	t.Parallel()
	// Arrange a projection with nested image and JSON data.
	service := newTestModel(t, AvailabilityIdle, nil)
	service.model.state.Transcript = []Line{
		{
			Kind:     LineUser,
			Text:     mo.Some("image"),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.Some(
				[]Content{{Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some([]byte{1, 2})}},
			),
		},
	}
	service.model.state.ActiveToolCalls = map[string]ToolCallState{"call": {
		CallID:      "call",
		Name:        "read",
		Position:    0,
		Provisional: true,
		Fields: []ToolCallField{
			{Name: "path", Value: mo.Some[any](map[string]any{"nested": []any{"original"}}), Prefix: mo.None[string]()},
		},
		Arguments: nil,
	}}
	first := displaySnapshot(t, service)

	// Act by changing application state after publishing the snapshot.
	require.NoError(t, service.Notify(plugininput.Notification{
		FailureCode: "",
		Kind:        plugininput.NotificationConnection,
		OperationID: "",
		Payload: mo.Some(
			plugininput.TextPayload(
				plugininput.TextUpdate{FailureCode: "", Kind: plugininput.TextInformation, Text: "later"},
			),
		),
		Failure: nil,
	}))
	service.Key(tuiinput.Key{Code: 0, Text: "draft", Mod: 0})

	// Assert the old snapshot stays coherent and cannot mutate private state through nested references.
	require.Len(t, first.Body.Transcript, 1)
	require.Empty(t, first.Input)
	first.Body.Transcript[0].Contents.OrEmpty()[0].Data.OrEmpty()[0] = 9
	first.Body.ActiveToolCalls["call"].Fields[0].Value.OrEmpty().(map[string]any)["nested"].([]any)[0] = "changed"
	require.Equal(t, byte(1), service.model.state.Transcript[0].Contents.OrEmpty()[0].Data.OrEmpty()[0])
	require.Equal(
		t,
		"original",
		service.model.state.ActiveToolCalls["call"].Fields[0].Value.OrEmpty().(map[string]any)["nested"].([]any)[0],
	)
	require.Len(t, service.model.state.Transcript, 2)
	require.Equal(t, "draft", string(service.model.input))
}

// TestTranscriptExpansionStaysApplicationOwned preserves display shortcuts while command I/O is pending.
func TestTranscriptExpansionStaysApplicationOwned(t *testing.T) {
	t.Parallel()
	// Arrange an application awaiting command acknowledgement.
	service := newTestModel(t, AvailabilityIdle, nil)
	service.model.emitting = true
	// Act by applying both expansion shortcuts twice without starting Host work.
	for _, key := range []rune{'t', 'o'} {
		require.Nil(t, service.Key(tuiinput.Key{Code: key, Text: "", Mod: tuiinput.ModCtrl}))
	}
	expanded := displaySnapshot(t, service)
	for _, key := range []rune{'t', 'o'} {
		require.Nil(t, service.Key(tuiinput.Key{Code: key, Text: "", Mod: tuiinput.ModCtrl}))
	}
	collapsed := displaySnapshot(t, service)
	// Assert one owner publishes coherent toggle state without changing pending command state.
	require.True(t, expanded.ReasoningExpanded)
	require.True(t, expanded.BranchSummariesExpanded)
	require.False(t, collapsed.ReasoningExpanded)
	require.False(t, collapsed.BranchSummariesExpanded)
	require.True(t, service.model.emitting)
}

// BenchmarkEditorSnapshot measures editor-only publication without transcript-size-dependent copying.
func BenchmarkEditorSnapshot(b *testing.B) {
	for _, count := range []int{100, 10_000, 100_000} {
		b.Run(fmt.Sprintf("transcript_%d", count), func(b *testing.B) {
			// Arrange a fixed editor and an already-published transcript.
			service := newTestModel(b, AvailabilityIdle, nil)
			service.model.state.Transcript = make([]Line, count)
			service.model.input = []rune("draft")
			service.model.projectionChanged = true
			service.publish()
			key := tuiinput.Key{Code: tuiinput.KeyLeft, Text: "", Mod: 0}
			// Act by publishing editor-only transitions repeatedly.
			b.ReportAllocs()
			for b.Loop() {
				service.Key(key)
			}
			// Assert the benchmark retained its transcript and editor.
			require.Len(b, service.model.state.Transcript, count)
			require.Equal(b, "draft", string(service.model.input))
		})
	}
}
