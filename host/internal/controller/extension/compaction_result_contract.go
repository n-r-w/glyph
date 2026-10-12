package extension

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// CompactionResultCommand contains the policy's selected result invocation.
type CompactionResultCommand struct {
	// OperationID identifies policy authority.
	OperationID string
	// Participant identifies the captured capability.
	Participant CompactionParticipant
	// OriginalRequest contains the initial request.
	OriginalRequest CompactionRequestValue
	// CurrentRequest contains the final request composition.
	CurrentRequest CompactionRequestValue
	// OriginalResult contains the result entering composition.
	OriginalResult session.CompactionEntry
	// CurrentResult contains the last accepted result.
	CurrentResult session.CompactionEntry
}

// CompactionResultResponse contains a participant's typed proposed action.
type CompactionResultResponse struct {
	// Cancel is the flag for exclusive cancellation.
	Cancel bool
	// ResultAction identifies preservation or replacement.
	ResultAction CompactionResultChange
	// Result contains the replacement candidate.
	Result mo.Option[session.CompactionEntry]
}

// encodePolicyResultAction converts the action for public completion.
func (r CompactionResultResponse) encodePolicyResultAction() *extensionpb.CompactionResultAction {
	response := extensionpb.CompactionResultAction_builder{
		Cancel:       new(r.Cancel),
		ResultAction: new(extensionpb.CompactionResultDisposition(r.ResultAction)),
		Result:       nil,
	}.Build()
	if result, present := r.Result.Get(); present {
		candidate := extensionpb.CompactionResult_builder{
			Summary:          new(result.Summary),
			FirstKeptEntryId: new(result.FirstKeptEntryID),
			Source:           mapCompactionSource(result.Source),
			Details:          nil,
		}.Build()
		if details, hasDetails := result.Details.Get(); hasDetails {
			candidate.SetDetails(append([]byte{}, details...))
		}
		response.SetResult(candidate)
	}
	return response
}
