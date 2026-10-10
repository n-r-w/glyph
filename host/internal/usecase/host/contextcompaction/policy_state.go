// Package contextcompaction owns conversation context sizing and compaction coordination.
package contextcompaction

import (
	"github.com/samber/mo"
)

// PolicyState contains acquired original and last valid composed values.
type PolicyState struct {
	// OriginalRequest contains the producer's immutable original request.
	OriginalRequest Request
	// CurrentRequest contains the last valid composed request.
	CurrentRequest Request
	// Result contains the last valid result, when present.
	Result mo.Option[Result]
}

// Clone detaches the acquired diagnostic values.
func (s PolicyState) Clone() PolicyState {
	s.OriginalRequest = cloneRequest(s.OriginalRequest)
	s.CurrentRequest = cloneRequest(s.CurrentRequest)
	s.Result = cloneOptionalResult(s.Result)
	return s
}
