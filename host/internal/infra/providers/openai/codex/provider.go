package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

const (
	requestFailedMessage   = "OpenAI Codex request failed."
	requestCanceledMessage = "OpenAI Codex request was canceled."
)

// Generate streams one provider response into Agent Core values.
func (s *Service) Generate(
	ctx context.Context,
	request run.ModelRequest,
	handleUpdate run.ModelUpdateHandler,
) (agent.ModelResponse, error) {
	credentials, err := s.resolveCredentials(ctx)
	if err != nil {
		return failedModelResponse(safeErrorMessage(err)), err
	}
	params, err := s.requestParams(request)
	if err != nil {
		message := safeErrorMessage(err)
		return failedModelResponse(message), safeError(message)
	}

	baseTransport := s.options.httpClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	errorTransport := newErrorCaptureTransport(baseTransport)
	httpClient := &http.Client{
		Transport:     errorTransport,
		CheckRedirect: s.options.httpClient.CheckRedirect,
		Jar:           s.options.httpClient.Jar,
		Timeout:       s.options.httpClient.Timeout,
	}
	service := responses.NewResponseService(
		option.WithBaseURL(s.options.modelBaseURL),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	)
	stream := service.NewStreaming(
		ctx,
		params,
		option.WithAPIKey(credentials.AccessToken),
		option.WithHeader("chatgpt-account-id", credentials.AccountID),
		option.WithHeader("OpenAI-Beta", "responses=experimental"),
		option.WithHeader("originator", "glyph"),
		option.WithHeader("User-Agent", "glyph"),
	)
	defer func() {
		_ = stream.Close()
	}()

	for stream.Next() {
		event := stream.Current()
		if outputIndex, delta, isModelUpdate := streamModelUpdate(event); isModelUpdate {
			if outputIndex < 0 || outputIndex > int64(^uint(0)>>1) {
				return failedModelResponse(requestFailedMessage), safeError(requestFailedMessage)
			}
			handlerErr := handleUpdate(run.ModelUpdate{Position: int(outputIndex), Delta: delta})
			if handlerErr != nil {
				return failedModelResponse("OpenAI Codex stream delivery failed."), handlerErr
			}
			continue
		}
		switch event.Type {
		case "response.completed":
			return modelResponse(event.AsResponseCompleted().Response, agent.ModelOutcomeStop)
		case "response.incomplete":
			incomplete := event.AsResponseIncomplete().Response
			if incomplete.IncompleteDetails.Reason == "max_output_tokens" {
				return modelResponse(incomplete, agent.ModelOutcomeLength)
			}
			message := providerFailureMessage(incomplete.Error.Message)
			return failedResponseFromSDK(incomplete, message)
		case "response.failed":
			failed := event.AsResponseFailed().Response
			message := providerFailureMessage(failed.Error.Message)
			return failedResponseFromSDK(failed, message)
		case "error":
			providerEvent := event.AsError()
			message := providerFailureMessage(providerEvent.Message)
			return failedModelResponse(message), safeError(message)
		}
	}
	streamErr := stream.Err()
	if streamErr != nil {
		return s.streamError(ctx, streamErr, errorTransport)
	}
	return failedModelResponse(requestFailedMessage), safeError(requestFailedMessage)
}

// requestParams builds one stateless Responses request from projected Agent Core values.
// streamModelUpdate maps every official incremental text event to one provider-neutral fragment.
func streamModelUpdate(event responses.ResponseStreamEventUnion) (position int64, delta string, ok bool) {
	switch event.Type {
	case "response.output_text.delta":
		delta := event.AsResponseOutputTextDelta()
		return delta.OutputIndex, delta.Delta, true
	case "response.refusal.delta":
		delta := event.AsResponseRefusalDelta()
		return delta.OutputIndex, delta.Delta, true
	default:
		return 0, "", false
	}
}

// requestParams maps one provider-neutral Agent Core request to an ordered Codex Responses request.
func (s *Service) requestParams(request run.ModelRequest) (responses.ResponseNewParams, error) {
	if s.config.Model == "" || request.Instructions == "" {
		return responses.ResponseNewParams{}, errors.New("OpenAI Codex model and request instructions are required")
	}
	input, err := buildInput(request.History)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	tools, err := buildTools(request.Tools)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	//nolint:exhaustruct // Optional SDK request fields intentionally use zero values.
	params := responses.ResponseNewParams{
		Model:             s.config.Model,
		Instructions:      param.NewOpt(request.Instructions),
		Store:             param.NewOpt(false),
		ParallelToolCalls: param.NewOpt(false),
		Include: []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		},
		Input: responses.ResponseNewParamsInputUnion{ //nolint:exhaustruct // Ordered input excludes string shorthand.
			OfInputItemList: input,
		},
		Reasoning: shared.ReasoningParam{ //nolint:exhaustruct // Optional SDK reasoning fields use zero values.
			Summary: shared.ReasoningSummaryAuto,
		},
		Tools: tools,
	}
	if s.config.ThinkingLevel != "" {
		params.Reasoning.Effort = shared.ReasoningEffort(s.config.ThinkingLevel)
	}
	return params, nil
}

// modelResponse converts supported SDK output items while preserving their order.
func modelResponse(response responses.Response, defaultOutcome agent.ModelOutcome) (agent.ModelResponse, error) {
	items := make([]agent.ModelItem, 0, len(response.Output))
	hasToolCall := false
	for outputIndex := range response.Output {
		output := &response.Output[outputIndex]
		switch output.Type {
		case "reasoning":
			reasoning := output.AsReasoning()
			summary := make([]string, len(reasoning.Summary))
			for index, item := range reasoning.Summary {
				summary[index] = item.Text
			}
			payload, err := json.Marshal(reasoningContext{
				ID: reasoning.ID, EncryptedContent: reasoning.EncryptedContent, Summary: summary,
			})
			if err != nil {
				return failedModelResponse(requestFailedMessage), fmt.Errorf("encode Codex reasoning context: %w", err)
			}
			items = append(items, agent.ModelItem{
				Kind: agent.ModelItemProviderContext, Text: "",
				ProviderContext: agent.ProviderContext{ProviderID: ProviderID, Payload: payload},
				ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
			})
		case "message":
			message := output.AsMessage()
			var text strings.Builder
			for contentIndex := range message.Content {
				content := &message.Content[contentIndex]
				switch content.Type {
				case "output_text":
					text.WriteString(content.AsOutputText().Text)
				case "refusal":
					text.WriteString(content.AsRefusal().Refusal)
				}
			}
			items = append(items, agent.ModelItem{
				Kind: agent.ModelItemText, Text: text.String(),
				ProviderContext: agent.ProviderContext{ProviderID: "", Payload: nil},
				ToolCall:        agent.ToolCall{ID: "", Name: "", Arguments: nil},
			})
		case "function_call":
			call := output.AsFunctionCall()
			var arguments map[string]any
			if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
				return failedModelResponse(requestFailedMessage), errors.New("OpenAI Codex returned invalid tool-call arguments")
			}
			items = append(items, agent.ModelItem{
				Kind: agent.ModelItemToolCall, Text: "",
				ProviderContext: agent.ProviderContext{ProviderID: "", Payload: nil},
				ToolCall:        agent.ToolCall{ID: call.CallID, Name: call.Name, Arguments: arguments},
			})
			hasToolCall = true
		default:
			return failedModelResponse(requestFailedMessage), fmt.Errorf(
				"OpenAI Codex returned unsupported output item %q",
				output.Type,
			)
		}
	}
	outcome := defaultOutcome
	if defaultOutcome == agent.ModelOutcomeStop && hasToolCall {
		outcome = agent.ModelOutcomeToolUse
	}
	return agent.ModelResponse{Items: items, Outcome: outcome, ErrorMessage: ""}, nil
}

// failedResponseFromSDK preserves finalized safe items while returning a failed outcome.
func failedResponseFromSDK(response responses.Response, message string) (agent.ModelResponse, error) {
	converted, err := modelResponse(response, agent.ModelOutcomeFailed)
	if err != nil {
		return failedModelResponse(message), errors.Join(safeError(message), err)
	}
	converted.Outcome = agent.ModelOutcomeFailed
	converted.ErrorMessage = message
	return converted, safeError(message)
}

// streamError maps cancellation, 401, and bounded provider details without replay.
func (s *Service) streamError(
	ctx context.Context,
	streamErr error,
	transport *errorCaptureTransport,
) (agent.ModelResponse, error) {
	if ctx.Err() != nil {
		return agent.ModelResponse{
			Items: nil, Outcome: agent.ModelOutcomeAborted, ErrorMessage: requestCanceledMessage,
		}, ctx.Err()
	}
	var apiError *openai.Error
	if errors.As(streamErr, &apiError) {
		if apiError.StatusCode == http.StatusUnauthorized {
			return failedModelResponse(signInRequiredMessage), ErrSignInRequired
		}
		detail := providerErrorDetail([]byte(apiError.RawJSON()))
		if detail == "" {
			detail = providerErrorDetail(transport.ErrorBody())
		}
		if detail == "" {
			detail = boundedDetail(strings.TrimSpace(apiError.Message))
		}
		message := providerFailureMessage(detail)
		return failedModelResponse(message), safeError(message)
	}
	return failedModelResponse(requestFailedMessage), safeError(requestFailedMessage)
}

// safeError removes terminal punctuation required only in user-facing model text.
func safeError(message string) error {
	return errors.New(strings.TrimRight(message, "."))
}

// providerFailureMessage adds bounded provider detail only when present.
func providerFailureMessage(detail string) string {
	detail = boundedDetail(strings.TrimSpace(detail))
	if detail == "" {
		return requestFailedMessage
	}
	return "OpenAI Codex request failed: " + detail
}

// safeErrorMessage retains safe local provider state errors without exposing payloads.
func safeErrorMessage(err error) string {
	if errors.Is(err, ErrSignInRequired) {
		return signInRequiredMessage
	}
	return boundedDetail(err.Error())
}

// failedModelResponse creates one terminal provider-neutral failure.
func failedModelResponse(message string) agent.ModelResponse {
	return agent.ModelResponse{Items: nil, Outcome: agent.ModelOutcomeFailed, ErrorMessage: message}
}
