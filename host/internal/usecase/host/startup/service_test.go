package startup

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

// synchronizedBuffer captures logger output safely while package tests run in parallel.
type synchronizedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

// Write appends one log record under the buffer lock.
func (buffer *synchronizedBuffer) Write(payload []byte) (int, error) {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.Write(payload)
}

// String returns a stable snapshot of captured log records.
func (buffer *synchronizedBuffer) String() string {
	buffer.mutex.Lock()
	defer buffer.mutex.Unlock()
	return buffer.buffer.String()
}

// TestServiceStartUsesDefaultAndOverrideDirectories verifies invocation overrides replace the default catalog.
func TestServiceStartUsesDefaultAndOverrideDirectories(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		request  Request
		expected toolservice.Directory
	}{
		"default": {
			request:  Request{DataDirectory: "/home/user/.glyph", ExtensionDirectory: ""},
			expected: toolservice.Directory{Path: filepath.Join("/home/user/.glyph", "plugins", "extension"), Explicit: false},
		},
		"override": {
			request:  Request{DataDirectory: "/home/user/.glyph", ExtensionDirectory: "/tmp/extensions"},
			expected: toolservice.Directory{Path: "/tmp/extensions", Explicit: true},
		},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			reporter := NewMockReporter(gomock.NewController(t))
			report := toolservice.LoadReport{Issues: nil, Extensions: nil}
			reporter.EXPECT().ReportSummary(t.Context(), report).Return(nil)
			service := New(
				func(_ context.Context, directory toolservice.Directory) (toolservice.LoadReport, error) {
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

// TestServiceLoadLogsExtensionCatalog records effective discovery and loaded plugin details.
func TestServiceLoadLogsExtensionCatalog(t *testing.T) {
	t.Parallel()

	var output synchronizedBuffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	report := toolservice.LoadReport{
		Issues: []toolservice.Issue{{PluginIDs: []string{"broken"}, Path: "/plugins/broken", Err: errors.New("start failed")}},
		Extensions: []toolservice.LoadedExtension{{
			ID: "glyph-tools", Path: "/plugins/glyph-tools",
			Tools: []tool.Descriptor{
				testStartupDescriptor("read"),
				testStartupDescriptor("bash"),
			},
		}},
	}
	service := New(func(_ context.Context, directory toolservice.Directory) (toolservice.LoadReport, error) {
		assert.Equal(t, toolservice.Directory{Path: "/plugins", Explicit: true}, directory)
		return report, nil
	})

	loaded, err := service.Load(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: "/plugins"})

	require.NoError(t, err)
	assert.Equal(t, report, loaded)
	logPayload := output.String()
	assert.Contains(t, logPayload, `"msg":"loading extensions"`)
	assert.Contains(t, logPayload, `"directory":"/plugins"`)
	assert.Contains(t, logPayload, `"explicit":true`)
	assert.Contains(t, logPayload, `"msg":"loaded extensions"`)
	assert.Contains(t, logPayload, `"extension_count":1`)
	assert.Contains(t, logPayload, `"issue_count":1`)
	assert.Contains(t, logPayload, `"msg":"loaded extension"`)
	assert.Contains(t, logPayload, `"plugin_id":"glyph-tools"`)
	assert.Contains(t, logPayload, `"path":"/plugins/glyph-tools"`)
	assert.Contains(t, logPayload, `"tools":["read","bash"]`)
}

// TestServiceStartReportsFailuresBeforeOneSummary verifies isolated failures and loaded ownership.
func TestServiceStartReportsFailuresBeforeOneSummary(t *testing.T) {
	t.Parallel()

	reporter := NewMockReporter(gomock.NewController(t))
	firstIssue := toolservice.Issue{PluginIDs: []string{"broken"}, Path: "/broken", Err: errors.New("start failed")}
	secondIssue := toolservice.Issue{PluginIDs: nil, Path: "/unreadable", Err: errors.New("unreadable default")}
	report := toolservice.LoadReport{
		Issues: []toolservice.Issue{firstIssue, secondIssue},
		Extensions: []toolservice.LoadedExtension{
			{ID: "first", Path: "/override/first", Tools: []tool.Descriptor{testStartupDescriptor("read")}},
			{ID: "second", Path: "/override/second", Tools: nil},
		},
	}
	gomock.InOrder(
		reporter.EXPECT().ReportIssue(t.Context(), firstIssue).Return(nil),
		reporter.EXPECT().ReportIssue(t.Context(), secondIssue).Return(nil),
		reporter.EXPECT().ReportSummary(t.Context(), report).Return(nil),
	)
	service := New(
		func(context.Context, toolservice.Directory) (toolservice.LoadReport, error) { return report, nil },
	)

	loaded, err := service.Start(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: ""}, reporter)

	require.NoError(t, err)
	assert.Equal(t, report, loaded)
}

func testStartupDescriptor(name string) tool.Descriptor {
	return tool.Descriptor{
		Name: name, Description: name, InputSchemaJSON: []byte(`{}`),
		ConstrainedSampling: tool.ConstrainedSampling{
			Kind: 0, JSONSchemaStrictness: 0,
			Grammar: tool.GrammarVariants{Lark: "", Regex: ""}, GrammarInputProperty: "",
		},
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
			func(context.Context, toolservice.Directory) (toolservice.LoadReport, error) {
				return toolservice.LoadReport{}, loadErr
			},
		)

		_, err := service.Start(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: "/missing"}, reporter)

		require.ErrorIs(t, err, loadErr)
	})

	t.Run("reporter error", func(t *testing.T) {
		t.Parallel()
		reporter := NewMockReporter(gomock.NewController(t))
		deliveryErr := errors.New("stderr failed")
		issue := toolservice.Issue{PluginIDs: []string{"broken"}, Path: "/broken", Err: errors.New("failed")}
		report := toolservice.LoadReport{Issues: []toolservice.Issue{issue}, Extensions: nil}
		reporter.EXPECT().ReportIssue(t.Context(), issue).Return(deliveryErr)
		service := New(
			func(context.Context, toolservice.Directory) (toolservice.LoadReport, error) { return report, nil },
		)

		_, err := service.Start(t.Context(), Request{DataDirectory: "/data", ExtensionDirectory: ""}, reporter)

		require.ErrorIs(t, err, deliveryErr)
	})
}
