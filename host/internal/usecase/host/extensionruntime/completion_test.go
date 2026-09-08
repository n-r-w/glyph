//go:build !integration

package extensionruntime

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestCompletionSurvivesRuntimeRemoval checks cleanup registration before runtimes leave the final state set.
func TestCompletionSurvivesRuntimeRemoval(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"registration", "rejection", "replacement", "final"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			// Arrange runtime completion independently of its ordinary registration result.
			controller := gomock.NewController(t)
			catalog := NewMockCatalog(controller)
			factory := NewMockRuntimeFactory(controller)
			runtime := NewMockExtensionRuntime(controller)
			cause := errors.New("retained runtime resource cleanup failure")
			catalog.EXPECT().Discover(gomock.Any(), gomock.Any()).Return(Discovery{
				DirectoryError: nil, Candidates: []Executable{{ID: "tools", Path: "/tools"}}, Issues: nil,
			}, nil).AnyTimes()
			factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(runtime, nil)
			var registerErr error
			if stage == "registration" {
				registerErr = errors.New("registration stopped")
			}
			runtime.EXPECT().Register(gomock.Any()).Return(Registration{Tools: nil, Handlers: nil}, registerErr)
			runtime.EXPECT().Close().Return(cause).Times(1)
			service := New(catalog, factory, newRuntimeReporter(t, nil))
			_, err := service.LoadPending(t.Context(), startup.Directory{})
			require.NoError(t, err)

			// Act through each owner cleanup path, then collect completion twice.
			switch stage {
			case "rejection":
				service.RejectPending([]string{"tools"})
			case "replacement":
				replacement := NewMockExtensionRuntime(controller)
				factory.EXPECT().Start(gomock.Any(), gomock.Any()).Return(replacement, nil)
				replacement.EXPECT().Register(gomock.Any()).Return(Registration{Tools: nil, Handlers: nil}, nil)
				replacement.EXPECT().Close().Return(nil).Times(1)
				_, err = service.LoadPending(t.Context(), startup.Directory{})
				require.NoError(t, err)
			}
			first, second := service.Close(), service.Close()

			// Assert removed runtimes retain their original failure and repeated collection does not duplicate it.
			require.ErrorIs(t, first, cause)
			require.ErrorIs(t, second, cause)
			require.Equal(t, 1, strings.Count(first.Error(), cause.Error()))
			require.Equal(t, first.Error(), second.Error())
		})
	}
}
