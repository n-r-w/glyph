package extension

import (
	"fmt"

	"github.com/n-r-w/glyph/internal/errtree"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// generatorFailureSource identifies the selected generator in source diagnostics.
	generatorFailureSource = "generator"
	// resultFailureSource identifies the selected result handler in source diagnostics.
	resultFailureSource = "result"
	// participantFailureCategoryFormat retains source categories beside their complete error text.
	participantFailureCategoryFormat = "%s category %s: %w"
)

// mapCompactionParticipantFailure retains each current source category before public failure mapping.
func mapCompactionParticipantFailure(source string, cause error) error {
	if isParticipantCancellation(cause) {
		return cause
	}
	categories := make(map[string]bool)
	for current := range errtree.IterateCurrentErrors(cause) {
		//nolint:errorlint // Current-node inspection excludes codes retained only in historical diagnostics.
		if failure, classified := current.(errtree.CodedError); classified {
			code := failure.Code()
			if !categories[code] {
				cause = fmt.Errorf(participantFailureCategoryFormat, source, code, cause)
				categories[code] = true
			}
		}
	}
	return extensionsdk.Fail(compactionFailureCode(cause), cause)
}
