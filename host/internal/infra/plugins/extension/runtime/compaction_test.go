//go:build !integration

package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestCompactionRequestReplacementRoundTripPreservesProjectedContent verifies later handlers observe public changes.
func TestCompactionRequestReplacementRoundTripPreservesProjectedContent(t *testing.T) {
	t.Parallel()
	// Arrange one replacement with handler-modified provider-neutral content.
	userContent := new(extensionpb.SessionTreeUserContent)
	userContent.SetText("changed by handler")
	entry := new(extensionpb.SessionTreeEntry)
	entry.SetUser(extensionpb.SessionTreeUserMessage_builder{
		Content: []*extensionpb.SessionTreeUserContent{userContent},
	}.Build())
	entry.SetId("entry")
	request := publicCompactionRequest(entry)

	// Act through the public-to-Host and Host-to-public mappings used between handlers.
	mapped, err := mapCompactionRequestFromProto(request)
	require.NoError(t, err)
	roundTrip, err := mapCompactionRequest(mapped)

	// Assert exact projected content remains visible to the next handler.
	require.NoError(t, err)
	require.True(t, roundTrip.HasContextTokensEstimated())
	require.True(t, roundTrip.GetContextTokensEstimated())
	require.Equal(t, "changed by handler", roundTrip.GetSuffix()[0].GetEntry().GetUser().GetContent()[0].GetText())
}

// TestCompactionRequestReplacementRequiresEstimateMarker verifies absent and false markers stop composition.
func TestCompactionRequestReplacementRequiresEstimateMarker(t *testing.T) {
	t.Parallel()
	// Arrange complete replacement requests whose required estimate marker is absent or false.
	for _, testCase := range []struct {
		name   string
		mutate func(*extensionpb.CompactionRequest)
	}{
		{name: "absent", mutate: func(request *extensionpb.CompactionRequest) {
			request.ClearContextTokensEstimated()
		}},
		{name: "false", mutate: func(request *extensionpb.CompactionRequest) {
			request.SetContextTokensEstimated(false)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request := publicCompactionRequest(nil)
			testCase.mutate(request)

			// Act through the public replacement boundary.
			_, err := mapCompactionRequestFromProto(request)

			// Assert later handlers cannot observe unmarked estimates.
			require.ErrorContains(t, err, "estimate marker")
		})
	}
}

// publicCompactionRequest creates one complete replacement request fixture.
func publicCompactionRequest(entry *extensionpb.SessionTreeEntry) *extensionpb.CompactionRequest {
	var suffix []*extensionpb.CompactionInputEntry
	if entry != nil {
		suffix = []*extensionpb.CompactionInputEntry{extensionpb.CompactionInputEntry_builder{
			Entry: entry, EstimatedTokens: new(int64(1)),
		}.Build()}
	}
	return extensionpb.CompactionRequest_builder{
		Trigger: new(extensionpb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL), RetryIntent: new(false),
		Instructions: nil,
		Model: extensionpb.ModelDescriptor_builder{
			ProviderId: new(
				"provider",
			),
			ModelId:         new("model"),
			InputModalities: []extensionpb.InputModality{extensionpb.InputModality_INPUT_MODALITY_TEXT},
			ContextWindow:   new(int64(1000)),
			MaxTokens:       new(int64(100)),
			Reasoning: extensionpb.ReasoningCapabilities_builder{
				Supported: new(false), Choices: []string{"off"}, DefaultChoice: new("off"),
			}.Build(),
			Tools:   nil,
			Pricing: nil,
		}.Build(),
		ReasoningChoice: new("off"), Prefix: nil, Suffix: suffix, Previous: nil,
		ContextTokens: new(int64(10)), ContextTokensEstimated: new(true),
		ContextWindow: new(int64(1000)), ResponseBudget: new(int64(100)), RetainedBudget: new(int64(20)),
	}.Build()
}

// TestCompactionResultActionRejectsInvalidDispositions verifies closed result actions cannot become replacements.
func TestCompactionResultActionRejectsInvalidDispositions(t *testing.T) {
	t.Parallel()
	// Arrange invalid clear, unspecified, and unknown dispositions with a replacement payload.
	source := new(extensionpb.BranchSummarySource)
	source.SetExtensionId("extension")
	result := extensionpb.CompactionResult_builder{
		Summary: new("summary"), FirstKeptEntryId: new("kept"), Source: source, Details: nil,
	}.Build()
	for name, disposition := range map[string]extensionpb.CompactionResultDisposition{
		"clear":       extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR,
		"unspecified": extensionpb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED,
		"unknown":     extensionpb.CompactionResultDisposition(99),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			action := extensionpb.CompactionResultAction_builder{
				Cancel: new(false), ResultAction: new(disposition), Result: result,
			}.Build()

			// Act at the Host public action boundary.
			_, err := mapCompactionResultAction(action)

			// Assert invalid dispositions are terminal rather than silently treated as replacement.
			require.ErrorContains(t, err, "result disposition")
		})
	}
}

// TestCompactionRequestReplacementRejectsMissingToolCallPayload verifies kind and payload stay consistent.
func TestCompactionRequestReplacementRejectsMissingToolCallPayload(t *testing.T) {
	t.Parallel()
	// Arrange a tool-call content kind without its required payload.
	content := extensionpb.SessionTreeModelContent_builder{
		Kind: new(extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL),
		Text: nil, ToolCall: nil,
	}.Build()
	entry := new(extensionpb.SessionTreeEntry)
	entry.SetModel(extensionpb.SessionTreeModelResponse_builder{
		Content: []*extensionpb.SessionTreeModelContent{content},
	}.Build())
	entry.SetId("entry")

	// Act through replacement entry validation.
	_, err := mapCompactionEntriesFromProto([]*extensionpb.CompactionInputEntry{
		extensionpb.CompactionInputEntry_builder{Entry: entry, EstimatedTokens: new(int64(1))}.Build(),
	})

	// Assert the malformed closed variant is rejected.
	require.ErrorContains(t, err, "tool-call payload")
}

// TestCompactionRequestReplacementRejectsMalformedToolArguments verifies invalid public state stops composition.
func TestCompactionRequestReplacementRejectsMalformedToolArguments(t *testing.T) {
	t.Parallel()
	// Arrange one replacement model entry with invalid JSON arguments.
	content := extensionpb.SessionTreeModelContent_builder{
		Kind: new(extensionpb.SessionTreeModelContentKind_SESSION_TREE_MODEL_CONTENT_KIND_TOOL_CALL), Text: nil,
		ToolCall: extensionpb.SessionTreeToolCall_builder{
			Id: new("call"), Name: new("tool"), ArgumentsJson: []byte(`{"broken":`),
		}.Build(),
	}.Build()
	entry := new(extensionpb.SessionTreeEntry)
	entry.SetModel(extensionpb.SessionTreeModelResponse_builder{
		Content: []*extensionpb.SessionTreeModelContent{content},
	}.Build())
	entry.SetId("entry")

	// Act through replacement entry validation.
	_, err := mapCompactionEntriesFromProto([]*extensionpb.CompactionInputEntry{
		extensionpb.CompactionInputEntry_builder{Entry: entry, EstimatedTokens: new(int64(1))}.Build(),
	})

	// Assert the complete argument validation cause is returned.
	require.ErrorContains(t, err, "validate tool call arguments")
}
