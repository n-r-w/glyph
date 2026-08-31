//go:build !integration

package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

// TestApplySynchronizesOneTreeMutation verifies complete write, sync, and close precede success.
func TestApplySynchronizesOneTreeMutation(t *testing.T) {
	t.Parallel()

	// Arrange one initial entry mutation and ordered filesystem expectations.
	controller := gomock.NewController(t)
	fileSystem := NewMockFileSystem(controller)
	file := NewMockFile(controller)
	project := t.TempDir()
	repository := New(t.TempDir(), project, fileSystem)
	createdAt := time.Unix(1, 0).UTC()
	entry := session.Entry{
		ID: "root", ParentID: mo.None[string](), CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage("root")),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	steps := make([]string, 0, 5)
	gomock.InOrder(
		fileSystem.EXPECT().OpenFile(
			repository.projectDirectory,
			gomock.Any(),
			os.O_WRONLY|os.O_CREATE|os.O_EXCL,
			os.FileMode(fileMode),
		).Return(file, nil),
		file.EXPECT().Chmod(os.FileMode(fileMode)).Return(nil),
		file.EXPECT().WritePayload(gomock.Any()).DoAndReturn(func(payload []byte) (int, error) {
			steps = append(steps, "write")
			require.Contains(t, string(payload), `"version":2`)
			require.Contains(t, string(payload), `"type":"entry"`)
			return len(payload), nil
		}),
		file.EXPECT().Sync().DoAndReturn(func() error { steps = append(steps, "sync"); return nil }),
		file.EXPECT().Close().DoAndReturn(func() error { steps = append(steps, "close"); return nil }),
	)

	// Act by applying the complete initial mutation.
	result, err := repository.Apply(t.Context(), hostsessions.ApplyCommand{
		Header:      session.Header{Version: 2, ID: "stored", CreatedAt: createdAt, WorkingDirectory: project},
		StoragePath: "",
		Mutation: hostsessions.Mutation{
			Entry:              mo.Some(entry),
			Navigation:         mo.None[hostsessions.NavigationMutation](),
			Label:              mo.None[hostsessions.LabelMutation](),
			SessionInformation: mo.None[hostsessions.SessionInformationMutation](),
		},
	})

	// Assert success follows exactly one complete durable mutation.
	require.NoError(t, err)
	require.NotEmpty(t, result.StoragePath)
	require.Equal(t, []string{"write", "sync", "close"}, steps)
}

// TestCreateSnapshotPreservesTreeIdentity verifies replacement snapshots retain tree state and provenance.
func TestCreateSnapshotPreservesTreeIdentity(t *testing.T) {
	t.Parallel()

	// Arrange a retained branch with a summary whose source boundary is unresolved locally.
	project := t.TempDir()
	root := t.TempDir()
	repository := New(root, project, realFileSystem{})
	require.NoError(t, repository.Initialize(t.Context()))
	createdAt := time.Unix(1, 0).UTC()
	informationUpdatedAt := createdAt.Add(time.Minute)
	rootEntry := session.Entry{
		ID: "root", ParentID: mo.None[string](), CreatedAt: createdAt,
		Information: mo.None[session.Information](), User: mo.Some(model.TextMessage("root")),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	summaryEntry := session.Entry{
		ID: "summary", ParentID: mo.Some("root"), CreatedAt: createdAt.Add(time.Second),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		BranchSummary: mo.Some(session.BranchSummaryEntry{
			Summary: "copied provenance", FirstEntryID: "source-first", LastEntryID: "source-last",
			Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow,
			Usage: mo.None[session.TokenUsage](), EstimatedCost: mo.None[session.EstimatedCost](),
		}),
	}
	tree, err := session.NewTree(
		[]session.Entry{rootEntry, summaryEntry}, mo.Some("summary"), map[string]string{"root": "kept"},
	)
	require.NoError(t, err)

	// Act by creating and replaying the replacement snapshot.
	result, err := repository.CreateSnapshot(t.Context(), hostsessions.CreateSnapshotCommand{
		Header: session.Header{Version: 2, ID: "replacement", CreatedAt: createdAt, WorkingDirectory: project},
		Tree:   tree, Information: mo.Some(session.Information{Name: "copy"}),
		InformationUpdatedAt: mo.Some(informationUpdatedAt),
	})
	require.NoError(t, err)
	loaded, err := repository.Load(t.Context(), "replacement")

	// Assert IDs, labels, leaf, and unresolved summary provenance survive.
	require.NoError(t, err)
	require.FileExists(t, result.StoragePath)
	require.Equal(
		t,
		[]string{"root", "summary"},
		lo.Map(loaded.Tree.Entries(), func(entry session.Entry, _ int) string {
			return entry.ID
		}),
	)
	require.Equal(t, mo.Some("summary"), loaded.Tree.ActiveLeafID())
	require.Equal(t, map[string]string{"root": "kept"}, loaded.Tree.Labels())
	require.Equal(t, mo.Some(informationUpdatedAt), loaded.InformationUpdatedAt)
	summary := loaded.Tree.Entries()[1].BranchSummary.MustGet()
	require.Equal(t, "source-first", summary.FirstEntryID)
	require.Equal(t, "source-last", summary.LastEntryID)
}

// realFileSystem applies the production file confinement adapter contract for repository tests.
type realFileSystem struct{}

func (realFileSystem) OpenFile(root, name string, flag int, permission os.FileMode) (File, error) {
	file, err := os.OpenFile(filepath.Join(root, name), flag, permission)
	return realFile{File: file}, err
}

// realFile maps the production filesystem calls to one operating-system file.
type realFile struct {
	// File owns the open file descriptor.
	*os.File
}

// ReadPayload reads bytes from the open test file.
func (file realFile) ReadPayload(payload []byte) (int, error) { return file.Read(payload) }

// WritePayload writes bytes to the open test file.
func (file realFile) WritePayload(payload []byte) (int, error) { return file.Write(payload) }
