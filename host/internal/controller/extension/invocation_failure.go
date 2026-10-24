package extension

import (
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/internal/errtree"
)

// qualifyingExtensionFailure returns the complete cause when a current error identifies an extension-operation failure.
func qualifyingExtensionFailure(cause error) error {
	for current := range errtree.IterateCurrentErrors(cause) {
		//nolint:errorlint // Current-node inspection must not classify historical diagnostic branches.
		if source, present := current.(NestedExtensionFailure); present && source.IsExtensionFailure() {
			return cause
		}
		//nolint:errorlint // Current-node inspection excludes failures retained only as diagnostic history.
		if _, present := current.(run.FatalOperationFailure); present {
			return cause
		}
	}
	return nil
}
