//go:build !integration

package presentation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

// TestLocalNameQueryPublishesProjection makes a local name or usage result visible in the same editor transition.
func TestLocalNameQueryPublishesProjection(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		// name identifies the local query variant.
		name string
		// storedName contains the Host-confirmed session name.
		storedName string
		// present distinguishes a named session from an unnamed one.
		present bool
		// expected is the name or usage result that must be published immediately.
		expected string
	}{
		{name: "named session", storedName: "selected session", present: true, expected: "selected session"},
		{name: "unnamed session", storedName: "", present: false, expected: slashCommandNameUsage},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange real application initialization and capture every actual output publication.
			controller := gomock.NewController(t)
			display := NewMockDisplay(controller)
			runtime := NewMockRuntime(controller)
			runtime.EXPECT().Open().Return(nil)
			runtime.EXPECT().Close().Return(nil)
			var published Snapshot
			display.EXPECT().Publish(gomock.Any()).AnyTimes().Do(func(snapshot Snapshot) { published = snapshot })
			service := New(NewMockHost(controller), display, runtime)
			work, err := service.PrepareInitialize(plugininput.Initialization{
				Availability: plugininput.AvailabilityIdle, Startup: nil, Models: nil,
				Selection: plugininput.ModelSelection{},
				Session: plugininput.SessionInfo{
					ID: "session", Name: testCase.storedName, NamePresent: testCase.present,
					WorkingDirectory: "/project", StoragePath: "", StoragePresent: false,
					CreatedAt: time.Time{}, UpdatedAt: time.Time{},
				},
			})
			require.NoError(t, err)
			require.NoError(t, work.Run(t.Context()))
			work.Release()
			t.Cleanup(func() { require.NoError(t, service.Close()) })

			// Act only through decoded terminal input, with no Host transport expectation.
			require.Nil(t, service.Key(tuiinput.Key{Code: 0, Text: slashCommandName, Mod: 0}))
			require.Nil(t, service.Key(tuiinput.Key{Code: tuiinput.KeyEnter, Text: "", Mod: 0}))

			// Assert this publication contains the local result and the cleared editor immediately.
			require.Len(t, published.Body.Transcript, 1)
			require.Equal(t, testCase.expected, published.Body.Transcript[0].Text.OrEmpty())
			require.Empty(t, published.Input)
			require.Zero(t, published.Cursor)
		})
	}
}
