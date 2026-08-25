package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	internalhooks "github.com/n-r-w/glyph/host/internal/hooks"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

var _ run.ModelProvider = (*Driver)(nil)

const (
	requestFailedMessage   = "OpenAI Codex request failed."
	requestCanceledMessage = "OpenAI Codex request was canceled."
)

// Stream emits one provider response as provider-neutral semantic events.
func (s *Driver) Stream(ctx context.Context, request run.ModelRequest, handle run.StreamHandler) error {
	var handlerErr error
	response, streamErr := s.generateResponse(ctx, request, func(event run.StreamEvent) error {
		if err := handle(event); err != nil {
			handlerErr = err
			return err
		}
		return nil
	})
	if handlerErr != nil {
		return handlerErr
	}
	response.Provider = request.Model.Provider
	response.Model = request.Model.Model
	terminalKind := run.StreamEventDone
	if streamErr != nil {
		terminalKind = run.StreamEventError
	}
	terminalEvent := semanticStreamEvent(terminalKind, 0, 0, "")
	terminalEvent.Response = response
	if err := handle(terminalEvent); err != nil {
		return err
	}
	return streamErr
}

// generateResponse decodes one Codex stream and returns its terminal response.
func (s *Driver) generateResponse(
	ctx context.Context,
	request run.ModelRequest,
	handle run.StreamHandler,
) (model.Response, error) {
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
	hookedTransport := &hookTransport{
		base: baseTransport, runner: s.hooks,
		provider: request.Model.Provider, model: request.Model.Model,
	}
	errorTransport := newErrorCaptureTransport(hookedTransport)
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
	assembler := newSemanticAssembler(handle, grammarInputProperties(request.Tools))

	for stream.Next() {
		response, terminal, terminalErr := assembler.consume(stream.Current())
		if terminal {
			return response, terminalErr
		}
	}
	if finishErr := assembler.finish(); finishErr != nil {
		return failedModelResponse("OpenAI Codex stream delivery failed."), finishErr
	}
	streamErr := stream.Err()
	if streamErr != nil {
		return s.streamError(ctx, streamErr, errorTransport)
	}
	return failedModelResponse(requestFailedMessage), safeError(requestFailedMessage)
}

// requestParams maps one provider-neutral Agent Core request to an ordered Codex Responses request.
func (s *Driver) requestParams(request run.ModelRequest) (responses.ResponseNewParams, error) {
	if request.Model.Provider != ProviderID || request.Model.Model == "" || request.Instructions == "" {
		return responses.ResponseNewParams{}, errors.New(
			"OpenAI Codex selected provider, model, and request instructions are required",
		)
	}
	tools, err := buildTools(request.Tools, requestToolCapabilities(request.Model))
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	input, err := buildInput(request.History, grammarInputProperties(request.Tools))
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	//nolint:exhaustruct // Optional SDK request fields intentionally use zero values.
	params := responses.ResponseNewParams{
		Model:             string(request.Model.Model),
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
	if request.ReasoningLevel != "" && request.ReasoningLevel != model.ReasoningLevelNone {
		params.Reasoning.Effort = shared.ReasoningEffort(request.ReasoningLevel)
	}
	return params, nil
}

// requestToolCapabilities maps provider-neutral support into Codex request selection.
func requestToolCapabilities(descriptor model.Descriptor) toolCapabilities {
	return toolCapabilities{
		strict: descriptor.ToolCapabilities.StrictJSONSchema,
		lark:   descriptor.ToolCapabilities.Grammar.Lark,
		regex:  descriptor.ToolCapabilities.Grammar.Regex,
	}
}

// modelResponse converts supported SDK output items while preserving their order.
//
//nolint:gocyclo // The flat branches convert the closed Codex output union.
func modelResponse(
	response responses.Response,
	defaultOutcome model.Outcome,
	grammarInputProperties map[string]string,
) (model.Response, error) {
	items := make([]model.Content, 0, len(response.Output))
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
			items = append(items,
				model.Content{
					Kind: model.ContentReasoning, Text: strings.Join(summary, ""), Final: true,
					ProviderContext: model.ProviderContext{ProviderID: "", Payload: nil},
					ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
				},
				model.Content{
					Kind: model.ContentProviderContext, Text: "", Final: true,
					ProviderContext: model.ProviderContext{ProviderID: ProviderID, Payload: payload},
					ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
				},
			)
		case "message":
			message := output.AsMessage()
			for contentIndex := range message.Content {
				content := &message.Content[contentIndex]
				var kind model.ContentKind
				var text string
				switch content.Type {
				case "output_text":
					kind = model.ContentText
					text = content.AsOutputText().Text
				case "refusal":
					kind = model.ContentRefusal
					text = content.AsRefusal().Refusal
				default:
					return failedModelResponse(requestFailedMessage), fmt.Errorf(
						"OpenAI Codex returned unsupported message content %q", content.Type,
					)
				}
				items = append(items, model.Content{
					Kind: kind, Text: text, Final: true,
					ProviderContext: model.ProviderContext{ProviderID: "", Payload: nil},
					ToolCall:        model.ToolCall{ID: "", Name: "", Arguments: nil},
				})
			}
		case "function_call":
			call := output.AsFunctionCall()
			var arguments map[string]any
			if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
				return failedModelResponse(requestFailedMessage), errors.New("OpenAI Codex returned invalid tool-call arguments")
			}
			items = append(items, model.Content{
				Kind: model.ContentToolCall, Text: "", Final: true,
				ProviderContext: model.ProviderContext{ProviderID: "", Payload: nil},
				ToolCall:        model.ToolCall{ID: call.CallID, Name: call.Name, Arguments: arguments},
			})
			hasToolCall = true
		case "custom_tool_call":
			call := output.AsCustomToolCall()
			property, ok := grammarInputProperties[call.Name]
			if !ok || property == "" {
				return failedModelResponse(requestFailedMessage), errors.New("OpenAI Codex returned an undeclared custom tool call")
			}
			items = append(items, model.Content{
				Kind: model.ContentToolCall, Text: "", Final: true,
				ProviderContext: model.ProviderContext{ProviderID: "", Payload: nil},
				ToolCall: model.ToolCall{
					ID: call.CallID, Name: call.Name, Arguments: map[string]any{property: call.Input},
				},
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
	if defaultOutcome == model.OutcomeStop && hasToolCall {
		outcome = model.OutcomeToolUse
	}
	var responseModel *model.ID
	if response.Model != "" {
		actualModel := model.ID(response.Model)
		responseModel = &actualModel
	}
	usage := model.Usage{
		InputTokens:       response.Usage.InputTokens,
		OutputTokens:      response.Usage.OutputTokens,
		CachedInputTokens: response.Usage.InputTokensDetails.CachedTokens,
		CacheWriteTokens:  response.Usage.InputTokensDetails.CacheWriteTokens,
		ReasoningTokens:   response.Usage.OutputTokensDetails.ReasoningTokens,
		TotalTokens:       response.Usage.TotalTokens,
	}
	return model.Response{
		Content: items, Outcome: outcome, ErrorMessage: "", Provider: "", Model: "",
		ResponseModel: responseModel, ResponseID: response.ID,
		Usage: usage, Diagnostics: nil,
	}, nil
}

// failedResponseFromSDK preserves finalized safe items while returning a failed outcome.
func failedResponseFromSDK(
	response responses.Response,
	message string,
	grammarInputProperties map[string]string,
) (model.Response, error) {
	converted, err := modelResponse(response, model.OutcomeFailed, grammarInputProperties)
	if err != nil {
		return failedModelResponse(message), errors.Join(safeError(message), err)
	}
	converted.Outcome = model.OutcomeFailed
	converted.ErrorMessage = message
	return converted, safeError(message)
}

// streamError maps cancellation, 401, and bounded provider details without replay.
func (s *Driver) streamError(
	ctx context.Context,
	streamErr error,
	transport *errorCaptureTransport,
) (model.Response, error) {
	if ctx.Err() != nil {
		response := emptyModelResponse()
		response.Outcome = model.OutcomeAborted
		response.ErrorMessage = requestCanceledMessage
		return response, ctx.Err()
	}
	var hookFailure internalhooks.HookError
	if errors.As(streamErr, &hookFailure) {
		return hookFailureResponse(hookFailure.Stage), hookFailure
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
func failedModelResponse(message string) model.Response {
	response := emptyModelResponse()
	response.Outcome = model.OutcomeFailed
	response.ErrorMessage = message
	return response
}

func emptyModelResponse() model.Response {
	return model.Response{
		Content: nil, Outcome: 0, ErrorMessage: "", Provider: "", Model: "", ResponseModel: nil, ResponseID: "",
		Usage: model.Usage{
			InputTokens: 0, OutputTokens: 0, CachedInputTokens: 0,
			CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
		},
		Diagnostics: nil,
	}
}
