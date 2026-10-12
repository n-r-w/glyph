package extensioncontext

import "github.com/samber/mo"

// ResultInvocation contains immutable original and composed participant inputs.
type ResultInvocation struct {
	// ParentCompactionID is the correlation ID for participant-owned nested work.
	ParentCompactionID mo.Option[string]
	// OriginalRequest contains the initial request.
	OriginalRequest Request
	// CurrentRequest contains the final request composition.
	CurrentRequest Request
	// OriginalResult contains the result entering composition.
	OriginalResult Result
	// CurrentResult contains the last accepted result.
	CurrentResult Result
}

// ResultAction contains one participant's proposed result update.
type ResultAction struct {
	// Cancel is the flag for exclusive cancellation.
	Cancel bool
	// ResultAction identifies preservation or replacement.
	ResultAction ResultActionKind
	// Result contains a replacement candidate.
	Result mo.Option[Result]
}
