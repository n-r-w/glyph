// Package extension adapts compaction capabilities to the public Extension SDK.
package extension

import (
	"context"
	"errors"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	sdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// policyHandler identifies the bundled policy capability.
	policyHandler = "compaction-policy"
	// generatorHandler identifies the bundled summary generator.
	generatorHandler = "compaction-generator"
	// invalidArgument identifies unsupported handler inputs.
	invalidArgument = "INVALID_ARGUMENT"
)

// Service exposes policy and optional generation through SDK operations.
type Service struct {
	// policy owns composition and budget decisions.
	policy Policy
	// generator owns standard summary generation.
	generator Generator
	// enabled indicates whether to register the generator.
	enabled bool
}

var _ sdk.Service = (*Service)(nil)

// New assembles the extension controller.
func New(policy Policy, generator Generator, enabled bool) *Service {
	return &Service{policy: policy, generator: generator, enabled: enabled}
}

// PrepareRegister admits startup registration.
func (s *Service) PrepareRegister(context.Context, *pb.RegisterRequest) (sdk.RegisterOperation, error) {
	return &registration{enabled: s.enabled}, nil
}

// PrepareHandle admits supported policy and generator callbacks.
func (s *Service) PrepareHandle(ctx context.Context, request *pb.HandleRequest) (sdk.HandleOperation, error) {
	if request.GetHandlerId() == policyHandler {
		switch {
		case request.GetCompactionPolicy() != nil:
			if _, present := sdk.FindCompactionPolicy(ctx); !present {
				return nil, sdk.Reject(invalidArgument, errors.New("compaction policy authority is required"))
			}
		case request.GetCompletedConversation() != nil, request.GetCompactionOutgoingInput() != nil:
		default:
			return nil, sdk.Reject(invalidArgument, errors.New("compaction policy payload is required"))
		}
	} else if request.GetHandlerId() != generatorHandler || !s.enabled || request.GetCompactionGenerate() == nil {
		return nil, sdk.Reject(invalidArgument, errors.New("unknown compaction capability or payload"))
	}
	return &handler{service: s, request: request}, nil
}

// PrepareExecute rejects tool execution because this extension registers handlers.
func (s *Service) PrepareExecute(context.Context, *pb.ExecuteRequest) (sdk.ExecuteOperation, error) {
	return nil, sdk.Reject(invalidArgument, errors.New("compaction extension registers no tools"))
}

// registration owns the extension's startup capability declaration.
type registration struct {
	// enabled indicates whether to register the default generator.
	enabled bool
}

var _ sdk.RegisterOperation = (*registration)(nil)

// Run declares the standard policy and optional generator.
func (r *registration) Run(context.Context) (*pb.RegisterResponse, error) {
	handlers := []*pb.HandlerDescriptor{
		pb.HandlerDescriptor_builder{
			Id:   new(policyHandler),
			Kind: new(pb.HandlerKind_HANDLER_KIND_COMPACTION_POLICY),
		}.Build(),
	}
	if r.enabled {
		handlers = append(
			handlers,
			pb.HandlerDescriptor_builder{
				Id:   new(generatorHandler),
				Kind: new(pb.HandlerKind_HANDLER_KIND_COMPACTION_GENERATE),
			}.Build(),
		)
	}
	return pb.RegisterResponse_builder{Tools: nil, Handlers: handlers}.Build(), nil
}

// Release is the cleanup hook for registration operations.
func (r *registration) Release() {}

// handler owns one admitted callback.
type handler struct {
	// service supplies the extension's use cases.
	service *Service
	// request contains the SDK-owned callback payload.
	request *pb.HandleRequest
}

var _ sdk.HandleOperation = (*handler)(nil)

// Run returns the typed acknowledgement for a callback.
func (h *handler) Run(ctx context.Context) (*pb.HandleResponse, error) {
	response := new(pb.HandleResponse)
	switch {
	case h.request.GetCompletedConversation() != nil:
		if err := h.service.policy.Observe(
			ctx,
			decodeObservation(h.request.GetCompletedConversation(), decodeBinding(h.request.GetContext())),
		); err != nil {
			return nil, err
		}
		response.SetCompletedConversation(new(pb.CompactionObserverAction))
	case h.request.GetCompactionOutgoingInput() != nil:
		if err := h.service.policy.ValidateOutgoing(
			ctx,
			decodeOutgoing(h.request.GetCompactionOutgoingInput()),
		); err != nil {
			return nil, err
		}
		response.SetCompactionOutgoingInput(new(pb.CompactionObserverAction))
	case h.request.GetCompactionGenerate() != nil:
		request := decodeRequest(h.request.GetCompactionGenerate().GetCurrent())
		if err := request.Validate(); err != nil {
			return nil, err
		}
		result, err := h.service.generator.Generate(ctx, request)
		if err != nil {
			return nil, err
		}
		response.SetCompactionGenerate(EncodeResult(result))
	case h.request.GetCompactionPolicy() != nil:
		input := decodeInput(h.request.GetCompactionPolicy())
		input.Binding = decodeBinding(h.request.GetContext())
		outcome, err := h.service.policy.Compact(ctx, input)
		result := new(pb.CompactionPolicyResult)
		if state, present := outcome.State.Get(); present {
			result.SetState(encodeState(state))
		}
		switch {
		case err != nil:
			result.SetError(err.Error())
		case outcome.Canceled:
			result.SetDisposition(pb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_CANCELED)
		case outcome.Preserve:
			result.SetDisposition(pb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_PRESERVE)
		default:
			result.SetDisposition(pb.CompactionPolicyDisposition_COMPACTION_POLICY_DISPOSITION_REPLACED)
		}
		response.SetCompactionPolicy(result)
	}
	return response, nil
}

// Release is the cleanup hook for callback operations.
func (h *handler) Release() {}
