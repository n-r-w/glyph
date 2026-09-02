//go:build !integration

package programmatic

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/samber/mo"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// testResponse creates an exhaustive empty internal response for one completed kind.
func testResponse(kind ResponseKind) Response {
	return Response{
		OperationID: "", Kind: kind,
		State: mo.None[RunStateResult](), Messages: nil, Models: mo.None[ModelsResult](),
		Selection: mo.None[model.Selection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](),
		SessionTree: mo.None[SessionTree](), TreeNavigation: mo.None[TreeNavigationResult](),
		Replacement: mo.None[SessionReplacement](), Rejection: mo.None[Rejection](),
		CancelTargetState: mo.None[operation.TerminalState](),
	}
}

// testRequest creates one operation envelope.
func testRequest(id string, set func(*programmaticv1.ControllerRequest)) *programmaticv1.OpenRequest {
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId(id)
	payload := new(programmaticv1.ControllerRequest)
	set(payload)
	request.SetRequest(payload)
	return request
}

// streamHarness provides channel-controlled receipt and records writer delivery through GoMock.
type streamHarness struct {
	stream    *MockOpenStream
	requests  chan *programmaticv1.OpenRequest
	responses chan *programmaticv1.OpenResponse
	closeOnce sync.Once
}

// newStreamHarness configures one mock stream with channel-controlled request EOF.
func newStreamHarness(t *testing.T, ctx context.Context) *streamHarness {
	t.Helper()
	stream := NewMockOpenStream(gomock.NewController(t))
	harness := &streamHarness{
		stream: stream, requests: make(chan *programmaticv1.OpenRequest, 64),
		responses: make(chan *programmaticv1.OpenResponse, 128), closeOnce: sync.Once{},
	}
	stream.EXPECT().Context().Return(ctx).AnyTimes()
	stream.EXPECT().Recv().DoAndReturn(func() (*programmaticv1.OpenRequest, error) {
		request, open := <-harness.requests
		if !open {
			return nil, io.EOF
		}
		return request, nil
	}).AnyTimes()
	stream.EXPECT().Send(gomock.Any()).DoAndReturn(func(response *programmaticv1.OpenResponse) error {
		harness.responses <- response
		return nil
	}).AnyTimes()
	return harness
}

// closeSend half-closes the mock controller request stream.
func (h *streamHarness) closeSend() {
	h.closeOnce.Do(func() { close(h.requests) })
}
