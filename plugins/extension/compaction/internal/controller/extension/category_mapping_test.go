//go:build !integration

package extension

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestPurposeMapping checks compaction policy purpose mapping at the boundary.
func TestPurposeMapping(t *testing.T) {
	t.Parallel()
	// Arrange every public purpose and unrecognized positive and negative values.
	cases := []struct {
		wire pb.CompactionPolicyPurpose
		want compaction.Purpose
	}{
		{wire: pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_UNSPECIFIED, want: compaction.PurposeUnspecified},
		{wire: pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_MANUAL, want: compaction.PurposeManual},
		{wire: pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_AUTOMATIC, want: compaction.PurposeAutomatic},
		{wire: pb.CompactionPolicyPurpose_COMPACTION_POLICY_PURPOSE_OVERFLOW, want: compaction.PurposeOverflow},
		{wire: pb.CompactionPolicyPurpose(91), want: compaction.PurposeInvalid},
		{wire: pb.CompactionPolicyPurpose(-1), want: compaction.PurposeInvalid},
	}
	for _, test := range cases {
		// Act through captured policy input decoding.
		wire := new(pb.CompactionPolicyInvocation)
		wire.SetPurpose(test.wire)
		decoded := decodeInput(wire)
		// Assert the extension receives a semantic category.
		require.Equal(t, test.want, decoded.Purpose)
		require.Equal(t, test.want, decodePurpose(test.wire))
	}
}

// TestTriggerMapping checks mapping and validation of compaction triggers.
func TestTriggerMapping(t *testing.T) {
	t.Parallel()
	// Arrange complete requests for every public trigger and unknown values.
	cases := []struct {
		wire  pb.CompactionTrigger
		want  compaction.Trigger
		valid bool
	}{
		{wire: pb.CompactionTrigger_COMPACTION_TRIGGER_UNSPECIFIED, want: compaction.TriggerUnspecified, valid: false},
		{wire: pb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL, want: compaction.TriggerManual, valid: true},
		{wire: pb.CompactionTrigger_COMPACTION_TRIGGER_THRESHOLD, want: compaction.TriggerThreshold, valid: true},
		{wire: pb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW, want: compaction.TriggerOverflow, valid: true},
		{wire: pb.CompactionTrigger(91), want: compaction.TriggerInvalid, valid: false},
		{wire: pb.CompactionTrigger(-1), want: compaction.TriggerInvalid, valid: false},
	}
	for _, test := range cases {
		wire := newMappingRequest()
		wire.SetTrigger(test.wire)
		wire.SetRetryIntent(test.wire == pb.CompactionTrigger_COMPACTION_TRIGGER_OVERFLOW)
		// Act through request decoding, validation, and encoding.
		decoded := decodeRequest(wire)
		// Assert valid categories round trip and invalid intent retains its failure text.
		require.Equal(t, test.want, decoded.Trigger)
		require.Equal(t, test.want, decodeTrigger(test.wire))
		if test.valid {
			require.NoError(t, decoded.Validate())
			require.Equal(t, test.wire, EncodeRequest(decoded).GetTrigger())
			require.Equal(t, test.wire, encodeTrigger(test.want))
		} else {
			require.ErrorContains(t, decoded.Validate(), "invalid compaction trigger or retry intent")
			require.Equal(t, pb.CompactionTrigger_COMPACTION_TRIGGER_UNSPECIFIED, encodeTrigger(test.want))
		}
	}
	request := decodeRequest(newMappingRequest())
	request.Retry = true
	require.ErrorContains(t, request.Validate(), "invalid compaction trigger or retry intent")
}

// TestModelModalityMapping checks model input modality mapping across the public boundary.
func TestModelModalityMapping(t *testing.T) {
	t.Parallel()
	// Arrange known, absent, and unknown input kinds.
	cases := []struct {
		wire    pb.InputModality
		want    compaction.Modality
		encoded pb.InputModality
	}{
		{
			wire:    pb.InputModality_INPUT_MODALITY_UNSPECIFIED,
			want:    compaction.ModalityUnspecified,
			encoded: pb.InputModality_INPUT_MODALITY_UNSPECIFIED,
		},
		{
			wire:    pb.InputModality_INPUT_MODALITY_TEXT,
			want:    compaction.ModalityText,
			encoded: pb.InputModality_INPUT_MODALITY_TEXT,
		},
		{
			wire:    pb.InputModality_INPUT_MODALITY_IMAGE,
			want:    compaction.ModalityImage,
			encoded: pb.InputModality_INPUT_MODALITY_IMAGE,
		},
		{
			wire:    pb.InputModality(91),
			want:    compaction.ModalityInvalid,
			encoded: pb.InputModality_INPUT_MODALITY_UNSPECIFIED,
		},
		{
			wire:    pb.InputModality(-1),
			want:    compaction.ModalityInvalid,
			encoded: pb.InputModality_INPUT_MODALITY_UNSPECIFIED,
		},
	}
	for _, test := range cases {
		wire := new(pb.ModelDescriptor)
		wire.SetInputModalities([]pb.InputModality{test.wire})
		// Act through descriptor decoding and encoding.
		model := decodeModel(wire)
		// Assert invalid input kinds remain invalid.
		require.Equal(t, []compaction.Modality{test.want}, model.Modalities)
		require.Equal(t, test.want, decodeModality(test.wire))
		require.Equal(t, []pb.InputModality{test.encoded}, encodeModel(model).GetInputModalities())
		require.Equal(t, test.encoded, encodeModality(test.want))
	}
}

// TestMessageVisibilityMapping checks client visibility mapping at the boundary.
func TestMessageVisibilityMapping(t *testing.T) {
	t.Parallel()
	// Arrange known, absent, and unknown transcript visibility.
	cases := []struct {
		wire    pb.ClientVisibility
		want    compaction.Visibility
		encoded pb.ClientVisibility
	}{
		{
			wire:    pb.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED,
			want:    compaction.VisibilityUnspecified,
			encoded: pb.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED,
		},
		{
			wire:    pb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE,
			want:    compaction.VisibilityVisible,
			encoded: pb.ClientVisibility_CLIENT_VISIBILITY_VISIBLE,
		},
		{
			wire:    pb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN,
			want:    compaction.VisibilityHidden,
			encoded: pb.ClientVisibility_CLIENT_VISIBILITY_HIDDEN,
		},
		{
			wire:    pb.ClientVisibility(91),
			want:    compaction.VisibilityInvalid,
			encoded: pb.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED,
		},
		{
			wire:    pb.ClientVisibility(-1),
			want:    compaction.VisibilityInvalid,
			encoded: pb.ClientVisibility_CLIENT_VISIBILITY_UNSPECIFIED,
		},
	}
	for _, test := range cases {
		message := new(pb.SessionTreeExtensionMessage)
		message.SetText("extension content")
		message.SetVisibility(test.wire)
		wire := new(pb.SessionTreeEntry)
		wire.SetExtensionMessage(message)
		// Act through entry decoding and encoding.
		entry := decodeEntry(wire)
		// Assert invalid visibility cannot become visible or hidden.
		require.Equal(t, test.want, entry.Visibility)
		require.Equal(t, test.want, decodeVisibility(test.wire))
		require.Equal(t, test.encoded, encodeEntry(entry).GetExtensionMessage().GetVisibility())
		require.Equal(t, test.encoded, encodeVisibility(test.want))
	}
}

// newMappingRequest supplies a complete request for category validation.
func newMappingRequest() *pb.CompactionRequest {
	prefix, suffix := new(pb.SessionTreeEntry), new(pb.SessionTreeEntry)
	prefix.SetId("prefix")
	suffix.SetId("suffix")
	content := new(pb.SessionTreeUserContent)
	content.SetText("source text")
	prefix.SetUser(pb.SessionTreeUserMessage_builder{Content: []*pb.SessionTreeUserContent{content}}.Build())
	suffix.SetUser(pb.SessionTreeUserMessage_builder{Content: []*pb.SessionTreeUserContent{content}}.Build())
	return pb.CompactionRequest_builder{
		Trigger:      new(pb.CompactionTrigger_COMPACTION_TRIGGER_MANUAL),
		RetryIntent:  new(false),
		Instructions: new(""),
		Model: pb.ModelDescriptor_builder{
			ProviderId:      new("provider"),
			ModelId:         new("model"),
			InputModalities: []pb.InputModality{pb.InputModality_INPUT_MODALITY_TEXT},
			ContextWindow:   new(int64(10000)),
			MaxTokens:       new(int64(100)),
			Reasoning: pb.ReasoningCapabilities_builder{
				Supported:     new(false),
				Choices:       []string{"off"},
				DefaultChoice: new("off"),
			}.Build(),
			Tools: pb.ToolCapabilities_builder{
				StrictJsonSchema: new(false),
				Lark:             new(false),
				Regex:            new(false),
			}.Build(),
			Pricing: nil,
		}.Build(),
		ReasoningChoice: new(
			"off",
		),
		Prefix: []*pb.CompactionInputEntry{
			pb.CompactionInputEntry_builder{Entry: prefix, EstimatedTokens: new(int64(1))}.Build(),
		},
		Suffix: []*pb.CompactionInputEntry{
			pb.CompactionInputEntry_builder{Entry: suffix, EstimatedTokens: new(int64(1))}.Build(),
		},
		Previous: nil,
		ContextTokens: new(
			int64(10),
		),
		ContextTokensEstimated: new(true),
		ContextWindow:          new(int64(10000)),
		ResponseBudget:         new(int64(100)),
		RetainedBudget:         new(int64(1)),
	}.Build()
}
