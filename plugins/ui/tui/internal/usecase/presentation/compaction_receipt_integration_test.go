//go:build integration

package presentation

import (
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
)

const (
	// receiptCompactionCommand starts the real manual-compaction presentation command.
	receiptCompactionCommand = "/compact"
	// receiptTreeCommand opens the committed tree projection.
	receiptTreeCommand = "/tree"
	// receiptNewSessionCommand requests a confirmed replacement session.
	receiptNewSessionCommand = "/new"
	// receiptReplacementSessionID identifies the replacement session used by the identity test.
	receiptReplacementSessionID = "receipt-replacement-session"
	// receiptWorkingDirectory identifies the sample project without filesystem access.
	receiptWorkingDirectory = "/receipt-project"
	// receiptEntryID identifies the Host-confirmed compaction marker.
	receiptEntryID = "retained-compaction"
	// receiptSecondEntryID identifies a distinct saved entry with the same sample summary.
	receiptSecondEntryID = "second-retained-compaction"
	// receiptOriginalID identifies the original active leaf.
	receiptOriginalID = "original-entry"
	// receiptPolicyID identifies the receipt's supplying policy.
	receiptPolicyID = "receipt-policy"
	// receiptPublishedLabel identifies richer metadata supplied only by publication.
	receiptPublishedLabel = "published-marker"
	// receiptFailureCode identifies the independent postcommit failure category.
	receiptFailureCode = "EXTENSION_FAILED"
	// receiptFailureText is the complete failure preserved through decoding and presentation.
	receiptFailureText = "compaction observer failed: complete original cause\nadditional publication cause"
	// receiptCancellationText preserves Completed owning-cancellation diagnostics.
	receiptCancellationText = "context canceled\nowning compaction cancellation cause"
	// receiptCancellationCode identifies the diagnostic-only compaction category.
	receiptCancellationCode = "COMPACTION_FAILED"
)

// TestCompactionReceiptNotification verifies receipt application, publication idempotence, diagnostics and settlement.
func TestCompactionReceiptNotification(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name       string
		published  bool
		openTree   bool
		failed     bool
		diagnostic string
	}{
		{name: "failed without publication", published: false, openTree: true, failed: true, diagnostic: receiptFailureText},
		{name: "failed after publication", published: true, openTree: true, failed: true, diagnostic: receiptFailureText},
		{name: "failed with closed tree", published: false, openTree: false, failed: true, diagnostic: receiptFailureText},
		{
			name: "failed after publication with closed tree", published: true, openTree: false,
			failed: true, diagnostic: receiptFailureText,
		},
		{
			name: "completed owning cancellation diagnostic", published: false, openTree: true,
			failed: false, diagnostic: receiptCancellationText,
		},
		{name: "healthy compaction", published: false, openTree: true, failed: false, diagnostic: ""},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange pending public commands through real decoder and presentation components.
			control := gomock.NewController(t)
			display := NewMockDisplay(control)
			var snapshot Snapshot
			var publishedSnapshots []Snapshot
			display.EXPECT().Publish(gomock.Any()).Do(func(value Snapshot) {
				snapshot = value
				publishedSnapshots = append(publishedSnapshots, value)
			}).AnyTimes()
			host := NewMockHost(control)
			expectedCalls := 1
			if scenario.openTree {
				expectedCalls++
			}
			host.EXPECT().Send(gomock.Any(), gomock.Any(), "").Return(nil).Times(expectedCalls)
			service := New(host, display, nil)
			require.NoError(t, service.Notify(plugininput.Notification{
				Kind: plugininput.NotificationConnection, OperationID: "", Failure: nil, FailureCode: "",
				Payload: mo.Some(plugininput.AvailabilityPayload(plugininput.AvailabilityIdle)),
			}))
			compactionID := dispatchReceiptCommand(t, service, &snapshot, receiptCompactionCommand)
			if scenario.openTree {
				openReceiptTree(t, service, &snapshot)
			}
			entry, published := compactionReceiptEntries()
			if scenario.published {
				event := new(uiv1.HostConnectionEvent)
				event.SetSessionEntryAdded(uiv1.SessionEntryAdded_builder{Entry: published}.Build())
				payload, err := plugininput.DecodeConnectionEvent(event)
				require.NoError(t, err)
				require.NoError(t, service.Notify(plugininput.Notification{
					Kind: plugininput.NotificationConnection, OperationID: "", Failure: nil, FailureCode: "",
					Payload: mo.Some(payload),
				}))
			}
			completed := new(uiv1.HostCompleted)
			var diagnostic, code *string
			if scenario.diagnostic != "" {
				diagnostic = new(scenario.diagnostic)
				code = new(receiptCancellationCode)
				if scenario.failed {
					code = new(receiptFailureCode)
				}
			}
			completed.SetCompaction(uiv1.CompactionResult_builder{
				Committed: entry, Canceled: new(false), Error: diagnostic, FailureCode: code,
			}.Build())

			// Act with a retained committed result alongside the complete terminal failure or owning-cancellation diagnostic.
			notification := applyReceiptCompletion(t, service, compactionID, completed, scenario.failed)

			// Assert settlement and published topology survive before checking the newly applied receipt summary.
			require.Error(t, service.Notify(notification))
			panel, present := snapshot.TreePanel.Get()
			require.Equal(t, scenario.openTree, present)
			markers := 0
			for _, row := range panel.Rows {
				if row.Entry.ID == receiptOriginalID && !scenario.published {
					require.True(t, row.ActiveLeaf)
				}
				if row.Entry.ID != receiptEntryID {
					continue
				}
				markers++
				require.Equal(t, TreeEntryCompaction, row.Entry.Kind)
				require.Equal(t, entry.GetCreatedTime().AsTime(), row.Entry.CreatedAt)
				require.Equal(t, entry.GetCompaction().GetSummary(), row.Entry.Text)
				if scenario.published {
					require.Equal(t, mo.Some(receiptOriginalID), row.Entry.ParentID)
					require.Equal(t, receiptPublishedLabel, row.Entry.Label)
				}
			}
			expectedMarkers := 0
			if scenario.published && scenario.openTree {
				expectedMarkers = 1
			}
			require.Equal(t, expectedMarkers, markers, "only publication supplies tree topology")
			if scenario.diagnostic != "" {
				require.NotEmpty(t, snapshot.Body.Transcript)
				last := snapshot.Body.Transcript[len(snapshot.Body.Transcript)-1]
				require.Equal(t, LineError, last.Kind)
				require.Equal(t, mo.Some(scenario.diagnostic), last.Text)
				for _, output := range publishedSnapshots {
					if len(output.Body.Transcript) == 0 ||
						output.Body.Transcript[len(output.Body.Transcript)-1].Text.OrEmpty() != scenario.diagnostic {
						continue
					}
					requireReceiptSummary(t, output, entry.GetCompaction().GetSummary())
				}
			} else {
				for _, line := range snapshot.Body.Transcript {
					require.NotEqual(t, LineError, line.Kind)
				}
			}
			requireReceiptSummary(t, snapshot, entry.GetCompaction().GetSummary())
		})
	}
}

// TestCompactionReceiptIdentity verifies identity-based application and receipt scope across session replacement.
func TestCompactionReceiptIdentity(t *testing.T) {
	t.Parallel()
	// Arrange a closed-panel presentation and two independent public compaction commands.
	control := gomock.NewController(t)
	display := NewMockDisplay(control)
	var snapshot Snapshot
	display.EXPECT().Publish(gomock.Any()).Do(func(value Snapshot) { snapshot = value }).AnyTimes()
	host := NewMockHost(control)
	host.EXPECT().Send(gomock.Any(), gomock.Any(), "").Return(nil).Times(4)
	service := New(host, display, nil)
	require.NoError(t, service.Notify(plugininput.Notification{
		Kind: plugininput.NotificationConnection, OperationID: "", Failure: nil, FailureCode: "",
		Payload: mo.Some(plugininput.AvailabilityPayload(plugininput.AvailabilityIdle)),
	}))

	// Act with different persisted identities and identical summary content.
	for _, id := range []string{receiptEntryID, receiptSecondEntryID} {
		operationID := dispatchReceiptCommand(t, service, &snapshot, receiptCompactionCommand)
		entry, _ := compactionReceiptEntries()
		entry.SetId(id)
		completed := new(uiv1.HostCompleted)
		completed.SetCompaction(uiv1.CompactionResult_builder{
			Committed: entry, Canceled: new(false), Error: nil, FailureCode: nil,
		}.Build())
		applyReceiptCompletion(t, service, operationID, completed, false)
	}

	// Assert the public transcript retains both actual entries rather than deduplicating their text.
	count := 0
	for _, line := range snapshot.Body.Transcript {
		if line.Kind == LineCompaction {
			count++
		}
	}
	require.Equal(t, 2, count)
	require.True(t, snapshot.TreePanel.IsNone())

	// Act after Host confirms a replacement session that may reuse persisted entry identities.
	replacementID := dispatchReceiptCommand(t, service, &snapshot, receiptNewSessionCommand)
	require.NoError(t, service.Notify(plugininput.Notification{
		Kind: plugininput.NotificationCompleted, OperationID: replacementID, Failure: nil, FailureCode: "",
		Payload: mo.Some(plugininput.SessionPayload(plugininput.SessionUpdate{
			Kind: plugininput.SessionChanged, Info: mo.Some(plugininput.SessionInfo{
				ID:               receiptReplacementSessionID,
				Name:             "",
				NamePresent:      false,
				WorkingDirectory: receiptWorkingDirectory,
				StoragePath:      "",
				StoragePresent:   false,
				CreatedAt:        time.Unix(3, 0),
				UpdatedAt:        time.Unix(3, 0),
			}),
			Sessions: nil, Transcript: nil, Statistics: mo.None[plugininput.SessionStatistics](),
		})),
	}))
	entry, _ := compactionReceiptEntries()
	completed := new(uiv1.HostCompleted)
	completed.SetCompaction(uiv1.CompactionResult_builder{
		Committed: entry, Canceled: new(false), Error: nil, FailureCode: nil,
	}.Build())
	operationID := dispatchReceiptCommand(t, service, &snapshot, receiptCompactionCommand)
	applyReceiptCompletion(t, service, operationID, completed, false)

	// Assert the replacement's receipt is applied even though its identity appeared in the previous session.
	requireReceiptSummary(t, snapshot, entry.GetCompaction().GetSummary())
}

// requireReceiptSummary checks one applied compaction summary in caller-visible transcript output.
func requireReceiptSummary(t *testing.T, snapshot Snapshot, summary string) {
	t.Helper()
	count := 0
	for _, line := range snapshot.Body.Transcript {
		if line.Kind == LineCompaction && line.Text.OrEmpty() == summary {
			count++
		}
	}
	require.Equal(t, 1, count, "the saved entry must have exactly one compaction summary")
}

// openReceiptTree applies an actual tree response to the pending public tree command.
func openReceiptTree(t *testing.T, service *Service, snapshot *Snapshot) {
	t.Helper()
	treeID := dispatchReceiptCommand(t, service, snapshot, receiptTreeCommand)
	original := uiv1.SessionTreeEntry_builder{
		Id: new(receiptOriginalID), ParentId: nil, CreatedTime: timestamppb.New(time.Unix(1, 0)), Label: nil,
		User: uiv1.UserMessage_builder{Content: []*uiv1.UserContent{
			uiv1.UserContent_builder{Text: new("original text"), Image: nil}.Build(),
		}}.Build(),
		Model: nil, ToolResult: nil, Extension: nil, BranchSummary: nil, ExtensionMessage: nil, Compaction: nil,
	}.Build()
	tree := new(uiv1.HostCompleted)
	tree.SetSessionTree(uiv1.SessionTreeResult_builder{Tree: uiv1.SessionTree_builder{
		Entries: []*uiv1.SessionTreeEntry{original}, ActiveLeafId: new(receiptOriginalID),
	}.Build()}.Build())
	applyReceiptCompletion(t, service, treeID, tree, false)
}

// dispatchReceiptCommand submits input through the public key and acknowledgement interfaces.
func dispatchReceiptCommand(t *testing.T, service *Service, snapshot *Snapshot, text string) string {
	t.Helper()
	clearReceiptSessionInput(t, service, *snapshot)
	require.Nil(t, service.Key(tuiinput.Key{Code: 0, Text: text, Mod: 0}))
	work := service.Key(tuiinput.Key{Code: tuiinput.KeyEnter, Text: "", Mod: 0})
	require.NotNil(t, work)
	result := work.Execute()
	require.NoError(t, result.Err)
	require.False(t, service.Complete(result))
	return result.ID
}

// applyReceiptCompletion decodes the retained result before forwarding the production notification contract.
func applyReceiptCompletion(
	t *testing.T, service *Service, id string, completed *uiv1.HostCompleted, failed bool,
) plugininput.Notification {
	t.Helper()
	payload, present, err := plugininput.DecodeCompleted(completed)
	require.NoError(t, err)
	require.True(t, present)
	notification := plugininput.Notification{
		Kind:        plugininput.NotificationCompleted,
		OperationID: id,
		Payload:     mo.Some(payload),
		Failure:     nil,
		FailureCode: "",
	}
	if failed {
		notification.Kind = plugininput.NotificationFailed
		notification.Failure = errors.New(completed.GetCompaction().GetError())
		notification.FailureCode = receiptFailureCode
	}
	require.NoError(t, service.Notify(notification))
	return notification
}

// compactionReceiptEntries supplies matching terminal and publication representations of one actual marker.
func compactionReceiptEntries() (*uiv1.SessionEntry, *uiv1.SessionTreeEntry) {
	source := new(uiv1.CompactionSource)
	source.SetExtensionId(receiptPolicyID)
	value := uiv1.Compaction_builder{
		Summary: new(
			"retained compaction summary",
		),
		FirstKeptEntryId: new(receiptOriginalID),
		Source:           source,
		Details:          nil,
	}.Build()
	created := timestamppb.New(time.Unix(2, 0))
	return uiv1.SessionEntry_builder{
			Id: new(receiptEntryID), CreatedTime: created, User: nil, Model: nil, ToolResult: nil,
			BranchSummary: nil, ExtensionMessage: nil, Compaction: value,
		}.Build(), uiv1.SessionTreeEntry_builder{
			Id: new(
				receiptEntryID,
			),
			ParentId:         new(receiptOriginalID),
			CreatedTime:      created,
			Label:            new(receiptPublishedLabel),
			User:             nil,
			Model:            nil,
			ToolResult:       nil,
			Extension:        nil,
			BranchSummary:    nil,
			ExtensionMessage: nil,
			Compaction:       value,
		}.Build()
}
