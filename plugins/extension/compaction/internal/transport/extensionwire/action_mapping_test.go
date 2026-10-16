//go:build !integration

package extensionwire

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestResultPayloadValidation checks payload and cancellation failure causes.
func TestResultPayloadValidation(t *testing.T) {
	t.Parallel()
	// Arrange incompatible result decisions and payload presence.
	cases := []struct {
		name          string
		decision      pb.CompactionResultDisposition
		payload       bool
		cancel        bool
		optionalError string
		existingError string
	}{
		{
			name:          "missing",
			decision:      pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE,
			payload:       false,
			cancel:        false,
			optionalError: "compaction result replacement payload does not match disposition",
			existingError: "compaction result replacement payload does not match disposition",
		},
		{
			name:          "unexpected",
			decision:      pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE,
			payload:       true,
			cancel:        false,
			optionalError: "compaction result replacement payload does not match disposition",
			existingError: "compaction result replacement payload does not match disposition",
		},
		{
			name:          "clear-payload",
			decision:      pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR,
			payload:       true,
			cancel:        false,
			optionalError: "compaction result replacement payload does not match disposition",
			existingError: "invalid compaction result disposition",
		},
		{
			name:          "canceled-payload",
			decision:      pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED,
			payload:       true,
			cancel:        true,
			optionalError: "compaction cancellation must be exclusive",
			existingError: "compaction cancellation must be exclusive",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request, result := new(pb.CompactionRequestAction), new(pb.CompactionResultAction)
			if !test.cancel {
				request.SetRequestAction(pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE)
			}
			request.SetResultAction(test.decision)
			result.SetResultAction(test.decision)
			request.SetCancel(test.cancel)
			result.SetCancel(test.cancel)
			if test.payload {
				request.SetResult(new(pb.CompactionResult))
				result.SetResult(new(pb.CompactionResult))
			}
			// Act through optional and existing result action decoding.
			requestErr := DecodeRequestAction(request).Validate()
			resultErr := DecodeResultAction(result).Validate()
			// Assert complete validation causes remain unchanged.
			require.EqualError(t, requestErr, test.optionalError)
			require.EqualError(t, resultErr, test.existingError)
		})
	}
	canceled := new(pb.CompactionRequestAction)
	canceled.SetCancel(true)
	canceled.SetRequest(newMappingRequest())
	require.EqualError(t, DecodeRequestAction(canceled).Validate(), "compaction cancellation must be exclusive")
}

// TestRequestDispositionMapping checks request decisions and cancellation exclusivity.
func TestRequestDispositionMapping(t *testing.T) {
	t.Parallel()
	// Arrange the public request decisions and unknown variants.
	cases := []struct {
		wire  pb.CompactionRequestDisposition
		want  compaction.RequestDisposition
		valid bool
	}{
		{
			wire:  pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_UNSPECIFIED,
			want:  compaction.RequestUnspecified,
			valid: false,
		},
		{
			wire:  pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE,
			want:  compaction.RequestPreserve,
			valid: true,
		},
		{
			wire:  pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE,
			want:  compaction.RequestReplace,
			valid: true,
		},
		{wire: pb.CompactionRequestDisposition(91), want: compaction.RequestInvalid, valid: false},
		{wire: pb.CompactionRequestDisposition(-1), want: compaction.RequestInvalid, valid: false},
	}
	for _, test := range cases {
		action := new(pb.CompactionRequestAction)
		action.SetRequestAction(test.wire)
		action.SetResultAction(pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE)
		if test.wire == pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE {
			action.SetRequest(newMappingRequest())
		}
		// Act through action decoding and validation.
		decoded := DecodeRequestAction(action)
		// Assert unknown decisions remain invalid, including alongside cancellation.
		require.Equal(t, test.want, decoded.RequestDisposition)
		require.Equal(t, test.want, decodeRequestDisposition(test.wire))
		if test.valid {
			require.NoError(t, decoded.Validate())
		} else {
			require.ErrorContains(t, decoded.Validate(), "invalid compaction request disposition")
		}
		canceled := new(pb.CompactionRequestAction)
		canceled.SetCancel(true)
		canceled.SetRequestAction(test.wire)
		cancelAction := DecodeRequestAction(canceled)
		if test.want == compaction.RequestUnspecified {
			require.NoError(t, cancelAction.Validate())
		} else {
			require.ErrorContains(t, cancelAction.Validate(), "compaction cancellation must be exclusive")
		}
	}
	missing := new(pb.CompactionRequestAction)
	missing.SetRequestAction(pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE)
	missing.SetResultAction(pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE)
	require.ErrorContains(
		t,
		DecodeRequestAction(missing).Validate(),
		"compaction request replacement payload does not match disposition",
	)
	missing.SetRequestAction(pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE)
	missing.SetRequest(newMappingRequest())
	require.ErrorContains(
		t,
		DecodeRequestAction(missing).Validate(),
		"compaction request replacement payload does not match disposition",
	)
}

// TestResultDispositionMapping checks clearing of optional results and restrictions on existing results.
func TestResultDispositionMapping(t *testing.T) {
	t.Parallel()
	// Arrange every public result decision and unknown variants.
	cases := []struct {
		wire     pb.CompactionResultDisposition
		want     compaction.ResultDisposition
		optional bool
		existing bool
	}{
		{
			wire:     pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_UNSPECIFIED,
			want:     compaction.ResultUnspecified,
			optional: false,
			existing: false,
		},
		{
			wire:     pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_PRESERVE,
			want:     compaction.ResultPreserve,
			optional: true,
			existing: true,
		},
		{
			wire:     pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE,
			want:     compaction.ResultReplace,
			optional: true,
			existing: true,
		},
		{
			wire:     pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_CLEAR,
			want:     compaction.ResultClear,
			optional: true,
			existing: false,
		},
		{wire: pb.CompactionResultDisposition(91), want: compaction.ResultInvalid, optional: false, existing: false},
		{wire: pb.CompactionResultDisposition(-1), want: compaction.ResultInvalid, optional: false, existing: false},
	}
	for _, test := range cases {
		request := new(pb.CompactionRequestAction)
		request.SetRequestAction(pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_PRESERVE)
		request.SetResultAction(test.wire)
		existing := new(pb.CompactionResultAction)
		existing.SetResultAction(test.wire)
		if test.wire == pb.CompactionResultDisposition_COMPACTION_RESULT_DISPOSITION_REPLACE {
			request.SetResult(new(pb.CompactionResult))
			existing.SetResult(new(pb.CompactionResult))
		}
		// Act through both optional-result and existing-result decoding.
		decodedRequest, decodedExisting := DecodeRequestAction(request), DecodeResultAction(existing)
		// Assert valid/invalid handling remains distinct from cancellation.
		require.Equal(t, test.want, decodedRequest.ResultDisposition)
		require.Equal(t, test.want, decodedExisting.Disposition)
		require.Equal(t, test.want, decodeResultDisposition(test.wire))
		if test.optional {
			require.NoError(t, decodedRequest.Validate())
		} else {
			require.ErrorContains(t, decodedRequest.Validate(), "invalid compaction result disposition")
		}
		if test.existing {
			require.NoError(t, decodedExisting.Validate())
		} else {
			require.ErrorContains(t, decodedExisting.Validate(), "invalid compaction result disposition")
		}
		canceled := new(pb.CompactionResultAction)
		canceled.SetCancel(true)
		canceled.SetResultAction(test.wire)
		canceledRequest := new(pb.CompactionRequestAction)
		canceledRequest.SetCancel(true)
		canceledRequest.SetResultAction(test.wire)
		if test.want == compaction.ResultUnspecified {
			require.NoError(t, DecodeResultAction(canceled).Validate())
			require.NoError(t, DecodeRequestAction(canceledRequest).Validate())
		} else {
			require.ErrorContains(
				t,
				DecodeResultAction(canceled).Validate(),
				"compaction cancellation must be exclusive",
			)
			require.ErrorContains(
				t,
				DecodeRequestAction(canceledRequest).Validate(),
				"compaction cancellation must be exclusive",
			)
		}
	}
}
