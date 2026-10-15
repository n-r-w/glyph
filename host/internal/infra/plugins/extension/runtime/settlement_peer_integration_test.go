//go:build integration

package runtime

import (
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// settlementPeerPrefix is the mode prefix for the peer's cancellation-settlement wire cases.
const settlementPeerPrefix = "shutdown-host-work-settlement-"

// serveSettlement sends the original terminal and cancellation result in the requested wire order.
func (s *adversarialServer) serveSettlement(
	stream extensionpb.ExtensionService_OpenServer,
	original *extensionpb.OpenRequest,
) error {
	if original.GetRequest().GetExecute() != nil && !strings.Contains(s.mode, "rejected") {
		if err := stream.Send(extensionResponse(original.GetOperationId(), toolProgressEvent())); err != nil {
			return err
		}
	}
	var cancellation *extensionpb.OpenRequest
	for {
		received, err := stream.Recv()
		if err != nil {
			return err
		}
		if received.GetRequest().GetCancel() != nil {
			cancellation = received
			break
		}
	}
	originalEvent := new(extensionpb.ExtensionEvent)
	switch {
	case strings.Contains(s.mode, "completed"):
		completed := new(extensionpb.ExtensionCompleted)
		if original.GetRequest().GetExecute() != nil {
			completed = toolCompletedEvent().GetCompleted()
		} else {
			response := new(extensionpb.HandleResponse)
			switch {
			case original.GetRequest().GetHandle().GetLifecycle() != nil:
				response.SetLifecycle(new(extensionpb.LifecycleAction))
			case original.GetRequest().GetHandle().GetCompactionFailure() != nil:
				response.SetCompactionFailure(new(extensionpb.CompactionObserverAction))
			case original.GetRequest().GetHandle().GetCompactionOutgoingInput() != nil:
				response.SetError(extensionpb.HandlerError_builder{Message: new(outgoingSemanticText)}.Build())
			default:
				response.SetSessionTree(new(extensionpb.SessionTreeAction))
			}
			completed.SetHandle(response)
		}
		originalEvent.SetCompleted(completed)
	case strings.Contains(s.mode, "malformed"):
		completed := new(extensionpb.ExtensionCompleted)
		if original.GetRequest().GetExecute() != nil {
			completed.SetTool(new(extensionpb.ToolResult))
		} else {
			completed.SetHandle(new(extensionpb.HandleResponse))
		}
		originalEvent.SetCompleted(completed)
	case strings.Contains(s.mode, "rejected"):
		originalEvent = rejectedExtensionEvent("BUSY", "original terminal independent rejection")
	case strings.Contains(s.mode, "failed"):
		originalEvent = failedExtensionEvent(fixtureInternalFailureCode, "original terminal independent failure")
	default:
		originalEvent.SetCanceled(
			operationpb.Canceled_builder{Message: new("original remote cancellation diagnostic")}.Build(),
		)
	}
	cancellationEvent := rejectedExtensionEvent("TARGET_NOT_ACTIVE", "original operation already settled")
	if strings.Contains(s.mode, "independent-cancel") {
		cancellationEvent = failedExtensionEvent(fixtureInternalFailureCode, outgoingInvocationText)
		if err := stream.Send(extensionResponse(cancellation.GetOperationId(), acceptedExtensionEvent())); err != nil {
			return err
		}
		if err := stream.Send(extensionResponse(cancellation.GetOperationId(), runningExtensionEvent())); err != nil {
			return err
		}
	}
	if strings.Contains(s.mode, "cancel-canceled") {
		cancellationEvent = new(extensionpb.ExtensionEvent)
		cancellationEvent.SetCanceled(
			operationpb.Canceled_builder{Message: new("cancellation request canceled diagnostic")}.Build(),
		)
		if err := stream.Send(extensionResponse(cancellation.GetOperationId(), acceptedExtensionEvent())); err != nil {
			return err
		}
		if err := stream.Send(extensionResponse(cancellation.GetOperationId(), runningExtensionEvent())); err != nil {
			return err
		}
	}
	first := extensionResponse(original.GetOperationId(), originalEvent)
	second := extensionResponse(cancellation.GetOperationId(), cancellationEvent)
	if strings.Contains(s.mode, "missing") {
		first = second
	}
	if strings.Contains(s.mode, "cancel-first") {
		first, second = second, first
	}
	if err := stream.Send(first); err != nil {
		return err
	}
	if strings.Contains(s.mode, "missing") {
		return status.Error(codes.Unavailable, "original terminal missing cause")
	}
	if err := stream.Send(second); err != nil {
		return err
	}
	// The peer serves later tool requests for the test's connection-usability check.
	for {
		request, err := stream.Recv()
		if err != nil {
			return err
		}
		if request.GetClose() != nil {
			return nil
		}
		if request.GetRequest().GetExecute() != nil {
			if err := sendAdversarialLifecycle(stream, request.GetOperationId(), toolCompletedEvent()); err != nil {
				return err
			}
		} else if request.GetRequest() != nil {
			return errors.New("unexpected request after settlement")
		}
	}
}
