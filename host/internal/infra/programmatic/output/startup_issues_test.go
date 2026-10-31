//go:build !integration

package output

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestBindWriterSendsStartupIssuesBeforeOtherEvents checks that held startup issues reach the client first, in
// report order and with their data mapped, ahead of an event published after the writer is bound.
func TestBindWriterSendsStartupIssuesBeforeOtherEvents(t *testing.T) {
	t.Parallel()

	// Arrange held issues with distinct extensions, texts, and both empty and non-empty catalog paths.
	output := New()
	output.HoldStartupIssues([]startup.Issue{
		{PluginIDs: []string{"alpha", "beta"}, Path: "", Err: errors.New("tool name conflicts")},
		{PluginIDs: []string{"gamma"}, Path: "/extensions/gamma", Err: errors.New("process exited")},
		{PluginIDs: []string{"delta"}, Path: "/extensions/delta", Err: errors.New("invalid schema")},
	})
	delivered := make([]*programmaticv1.OpenResponse, 0, 4)
	writer, err := operation.NewWriter(func(response *programmaticv1.OpenResponse) error {
		delivered = append(delivered, response)
		return nil
	})
	require.NoError(t, err)

	// Act by binding the writer, publishing one more connection event, then draining the writer.
	unbind := output.BindWriter(writer)
	defer unbind()
	_, err = output.PublishSelection(
		model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh},
	)
	require.NoError(t, err)
	writer.Close()
	require.NoError(t, writer.Run(t.Context()))

	// Assert the issues come first in held order with their mapped data, and the other event comes last.
	require.Len(t, delivered, 4)
	expected := []struct {
		extensionIDs []string
		catalogPath  string
		text         string
	}{
		{extensionIDs: []string{"alpha", "beta"}, catalogPath: "", text: "tool name conflicts"},
		{extensionIDs: []string{"gamma"}, catalogPath: "/extensions/gamma", text: "process exited"},
		{extensionIDs: []string{"delta"}, catalogPath: "/extensions/delta", text: "invalid schema"},
	}
	for index, want := range expected {
		got := delivered[index].GetConnectionEvent().GetStartupIssue()
		require.NotNil(t, got)
		assert.Equal(t, want.extensionIDs, got.GetExtensionIds())
		assert.Equal(t, want.catalogPath, got.GetCatalogPath())
		assert.Equal(t, want.text, got.GetText())
	}
	assert.True(t, delivered[3].GetConnectionEvent().HasModelSelectionChanged())
}
