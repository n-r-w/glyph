package extensioncontext

import "github.com/samber/mo"

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
	s.OriginalRequest = s.OriginalRequest.clone()
	s.CurrentRequest = s.CurrentRequest.clone()
	s.Result = s.Result.MapValue(Result.clone)
	return s
}
