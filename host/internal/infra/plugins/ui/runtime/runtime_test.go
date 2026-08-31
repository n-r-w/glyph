//go:build !integration

package runtime

import (
	"google.golang.org/grpc"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

//go:generate go tool mockgen -build_constraint=!integration -destination=stream_mock_test.go -package=runtime github.com/n-r-w/glyph/pkg/plugins/ui/v1 UIService_OpenClient

// runtimeContractService records Host frames and returns every supported UI command.
type runtimeContractService struct {
	uipb.UnimplementedUIServiceServer
	received chan *uipb.OpenRequest
}

// closeContractService holds one real gRPC receive open until its stream context is canceled.
type closeContractService struct {
	uipb.UnimplementedUIServiceServer
	opened chan struct{}
}

// Open reports stream readiness and waits for client-side cancellation.
func (s *closeContractService) Open(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse]) error {
	close(s.opened)
	<-stream.Context().Done()
	return stream.Context().Err()
}
