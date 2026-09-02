//go:build !integration

package programmatic

import programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"

// controllerRequest returns the mutable nested request payload used by tests.
func controllerRequest(request *programmaticv1.OpenRequest) *programmaticv1.ControllerRequest {
	if !request.HasRequest() {
		request.SetRequest(new(programmaticv1.ControllerRequest))
	}
	return request.GetRequest()
}
