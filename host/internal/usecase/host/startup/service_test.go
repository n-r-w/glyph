//go:build !integration

package startup

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	extensionservice "github.com/n-r-w/glyph/host/internal/usecase/host/extensions"
)

// TestServiceStartUsesDefaultAndOverrideDirectories verifies invocation overrides replace the default catalog.
func TestServiceStartUsesDefaultAndOverrideDirectories(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		request  Request
		expected extensionservice.Directory
	}{
		"default": {
			request: Request{DataDirectory: "/home/user/.glyph", ExtensionDirectory: ""},
			expected: extensionservice.Directory{
				Path:     filepath.Join("/home/user/.glyph", "plugins", "extension"),
				Explicit: false,
			},
		},
		"override": {
			request:  Request{DataDirectory: "/home/user/.glyph", ExtensionDirectory: "/tmp/extensions"},
			expected: extensionservice.Directory{Path: "/tmp/extensions", Explicit: true},
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reporter := NewMockReporter(gomock.NewController(t))
			report := extensionservice.LoadReport{Issues: nil, Extensions: nil}
			reporter.EXPECT().ReportSummary(t.Context(), report).Return(nil)
			service := New(
				func(_ context.Context, directory extensionservice.Directory) (extensionservice.LoadReport, error) {
					assert.Equal(t, testCase.expected, directory)
					return report, nil
				},
			)

			loaded, err := service.Start(t.Context(), testCase.request, reporter)

			require.NoError(t, err)
			assert.Equal(t, report, loaded)
		})
	}
}

// TestServiceStartReportsFailuresBeforeOneSummary verifies isolated failures and loaded ownership.
func TestServiceStartReportsFailuresBeforeOneSummary(t *testing.T) {
	t.Parallel()

	reporter := NewMockReporter(gomock.NewController(t))
	firstIssue := extensionservice.Issue{
		PluginIDs: []string{"broken"},
		Path:      "/broken",
		Err:       errors.New("start failed"),
	}
	secondIssue := extensionservice.Issue{PluginIDs: nil, Path: "/unreadable", Err: errors.New("unreadable default")}
	report := extensionservice.LoadReport{
		Issues: []extensionservice.Issue{firstIssue, secondIssue},
		Extensions: []extensionservice.LoadedExtension{
			{
				ID:       "first",
				Path:     "/override/first",
				Tools:    []tool.Descriptor{testStartupDescriptor("read")},
				Handlers: nil,
			},
			{ID: "second", Path: "/override/second", Tools: nil, Handlers: nil},
		},
	}
	gomock.InOrder(
		reporter.EXPECT().ReportIssue(t.Context(), firstIssue).Return(nil),
		reporter.EXPECT().ReportIssue(t.Context(), secondIssue).Return(nil),
		reporter.EXPECT().ReportSummary(t.Context(), report).Return(nil),
	)
	service := New(
		func(context.Context, extensionservice.Directory) (extensionservice.LoadReport, error) {
			return report, nil
		},
	)

	loaded, err := service.Start(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: ""}, reporter)

	require.NoError(t, err)
	assert.Equal(t, report, loaded)
}

func testStartupDescriptor(name string) tool.Descriptor {
	return tool.Descriptor{
		Name: name, Description: name, InputSchemaJSON: []byte(`{}`),
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
	}
}

// TestServiceStartPropagatesCatalogAndReporterFailures verifies startup stops without retry.
func TestServiceStartPropagatesCatalogAndReporterFailures(t *testing.T) {
	t.Parallel()

	t.Run("explicit catalog error", func(t *testing.T) {
		t.Parallel()
		reporter := NewMockReporter(gomock.NewController(t))
		loadErr := errors.New("explicit directory missing")
		service := New(
			func(context.Context, extensionservice.Directory) (extensionservice.LoadReport, error) {
				return extensionservice.LoadReport{}, loadErr
			},
		)

		_, err := service.Start(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: "/missing"}, reporter)

		require.ErrorIs(t, err, loadErr)
	})

	t.Run("reporter error", func(t *testing.T) {
		t.Parallel()
		reporter := NewMockReporter(gomock.NewController(t))
		deliveryErr := errors.New("stderr failed")
		issue := extensionservice.Issue{PluginIDs: []string{"broken"}, Path: "/broken", Err: errors.New("failed")}
		report := extensionservice.LoadReport{Issues: []extensionservice.Issue{issue}, Extensions: nil}
		reporter.EXPECT().ReportIssue(t.Context(), issue).Return(deliveryErr)
		service := New(
			func(context.Context, extensionservice.Directory) (extensionservice.LoadReport, error) {
				return report, nil
			},
		)

		_, err := service.Start(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: ""}, reporter)

		require.ErrorIs(t, err, deliveryErr)
	})
}
