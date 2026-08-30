package codex

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	internalhooks "github.com/n-r-w/glyph/host/internal/hooks"
	providerconsts "github.com/n-r-w/glyph/host/internal/infra/providers"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

var _ run.ModelProvider = (*Driver)(nil)

const (
	requestFailedMessage   = "OpenAI Codex request failed."
	requestCanceledMessage = "OpenAI Codex request was canceled."
	// responseItemTypeReasoning identifies provider reasoning output.
	responseItemTypeReasoning = "reasoning"
	// responseItemTypeFunctionCall identifies a standard provider tool call.
	responseItemTypeFunctionCall = "function_call"
	// responseItemTypeCustomToolCall identifies a custom provider tool call.
	responseItemTypeCustomToolCall = "custom_tool_call"
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
		return combineHandlerError(streamErr, handlerErr)
	}
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) && !errors.Is(streamErr, context.DeadlineExceeded) {
		errorMessage := response.ErrorMessage.OrEmpty()
		if errorMessage == "" {
			errorMessage = streamErr.Error()
		} else if !strings.Contains(errorMessage, streamErr.Error()) {
			errorMessage += ": " + streamErr.Error()
		}
		response.ErrorMessage = mo.Some(errorMessage)
	}
	response.Provider = mo.Some(request.Model.Provider)
	response.Model = mo.Some(request.Model.Model)
	if configured, ok := s.models[request.Model.Model]; ok {
		source := model.ProviderContextSource{
			ProviderID:       request.Model.Provider,
			API:              configured.api,
			Model:            request.Model.Model,
			CompatibilityKey: configured.reasoningCompatibilityKey,
		}
		for index := range response.Content {
			content := &response.Content[index]
			providerContext, hasProviderContext := content.ProviderContext.Get()
			if content.Kind == model.ContentReasoning && hasProviderContext && len(providerContext.Payload) != 0 {
				providerContext.Source = source
				content.ProviderContext = mo.Some(providerContext)
			}
		}
	}
	terminalKind := run.StreamEventDone
	if streamErr != nil {
		terminalKind = run.StreamEventError
	}
	terminalEvent := semanticStreamEvent(terminalKind, 0, 0, "")
	terminalEvent.Response = mo.Some(response)
	if err := handle(terminalEvent); err != nil {
		return combineHandlerError(streamErr, err)
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
		return terminalModelResponse(boundedErrorMessage(err), model.OutcomeFailed), err
	}
	params, err := s.requestParams(request)
	if err != nil {
		message := boundedErrorMessage(err)
		return terminalModelResponse(message, model.OutcomeFailed), safeError(message)
	}

	baseTransport := s.options.httpClient.Transport
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}
	hookedTransport := &hookTransport{
		base:     baseTransport,
		runner:   s.hooks,
		provider: request.Model.Provider,
		model:    request.Model.Model,
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
		option.WithHeader("originator", providerconsts.AgentID),
		option.WithHeader("User-Agent", providerconsts.AgentID),
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
	streamErr := stream.Err()
	if finishErr := assembler.finish(); finishErr != nil {
		return terminalModelResponse(
			"OpenAI Codex stream delivery failed.", model.OutcomeFailed,
		), errors.Join(streamErr, finishErr)
	}
	if streamErr != nil {
		return s.streamError(ctx, streamErr, errorTransport)
	}
	return terminalModelResponse(requestFailedMessage, model.OutcomeFailed), safeError(requestFailedMessage)
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
	configured, found := s.models[request.Model.Model]
	if !found {
		return responses.ResponseNewParams{}, errors.New("OpenAI Codex selected model is not configured")
	}
	target := model.ProviderContextSource{
		ProviderID:       request.Model.Provider,
		API:              configured.api,
		Model:            request.Model.Model,
		CompatibilityKey: configured.reasoningCompatibilityKey,
	}
	input, err := buildInput(request.History, grammarInputProperties(request.Tools), target)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	params := responses.ResponseNewParams{
		Model:             string(request.Model.Model),
		Instructions:      param.NewOpt(request.Instructions),
		Store:             param.NewOpt(false),
		ParallelToolCalls: param.NewOpt(false),
		Include: []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		},
		//nolint:exhaustruct_v5 // responses.ResponseNewParamsInputUnion sets only the active OfInputItemList field.
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
		Reasoning: shared.ReasoningParam{
			Summary: shared.ReasoningSummaryAuto,
			Context: "",
			Effort:  "",
			Mode:    "",
		},
		Tools:                tools,
		Background:           param.Opt[bool]{},
		MaxOutputTokens:      param.Opt[int64]{},
		MaxToolCalls:         param.Opt[int64]{},
		PreviousResponseID:   param.Opt[string]{},
		PromptCacheKey:       param.Opt[string]{},
		SafetyIdentifier:     param.Opt[string]{},
		Temperature:          param.Opt[float64]{},
		TopLogprobs:          param.Opt[int64]{},
		TopP:                 param.Opt[float64]{},
		User:                 param.Opt[string]{},
		ContextManagement:    nil,
		Conversation:         responses.ResponseNewParamsConversationUnion{},
		Metadata:             nil,
		Moderation:           responses.ResponseNewParamsModeration{},
		Prompt:               responses.ResponsePromptParam{},
		PromptCacheRetention: "",
		ServiceTier:          "",
		StreamOptions:        responses.ResponseNewParamsStreamOptions{},
		Truncation:           "",
		PromptCacheOptions:   responses.ResponseNewParamsPromptCacheOptions{},
		Text:                 responses.ResponseTextConfigParam{},
		ToolChoice:           responses.ResponseNewParamsToolChoiceUnion{},
	}
	switch request.ReasoningChoice {
	case model.ReasoningChoiceOff:
		params.Reasoning.Effort = shared.ReasoningEffortNone
	case model.ReasoningChoiceOn:
	case model.ReasoningChoiceMinimal, model.ReasoningChoiceLow, model.ReasoningChoiceMedium,
		model.ReasoningChoiceHigh, model.ReasoningChoiceXHigh, model.ReasoningChoiceMax:
		params.Reasoning.Effort = shared.ReasoningEffort(request.ReasoningChoice)
	default:
		return responses.ResponseNewParams{}, errors.New("OpenAI Codex reasoning choice is invalid")
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
		case responseItemTypeReasoning:
			visible, err := modelReasoningContent(output.AsReasoning())
			if err != nil {
				return terminalModelResponse(requestFailedMessage, model.OutcomeFailed), err
			}
			items = append(items, visible)
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
					return terminalModelResponse(requestFailedMessage, model.OutcomeFailed), fmt.Errorf(
						"OpenAI Codex returned unsupported message content %q", content.Type,
					)
				}
				items = append(items, model.Content{
					Kind:            kind,
					Text:            mo.Some(text),
					Final:           true,
					ProviderContext: mo.None[model.ProviderContext](),
					ToolCall:        mo.None[model.ToolCall](),
				})
			}
		case responseItemTypeFunctionCall:
			call := output.AsFunctionCall()
			var arguments map[string]any
			if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
				conversionErr := fmt.Errorf("decode OpenAI Codex tool-call arguments: %w", err)
				return terminalModelResponse(conversionErr.Error(), model.OutcomeFailed), conversionErr
			}
			items = append(items, model.Content{
				Kind:            model.ContentToolCall,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall: mo.Some(model.ToolCall{
					ID:        call.CallID,
					Name:      call.Name,
					Arguments: arguments,
				}),
			})
			hasToolCall = true
		case responseItemTypeCustomToolCall:
			call := output.AsCustomToolCall()
			property, ok := grammarInputProperties[call.Name]
			if !ok || property == "" {
				return terminalModelResponse(requestFailedMessage, model.OutcomeFailed), errors.New(
					"OpenAI Codex returned an undeclared custom tool call",
				)
			}
			items = append(items, model.Content{
				Kind:            model.ContentToolCall,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall: mo.Some(model.ToolCall{
					ID:        call.CallID,
					Name:      call.Name,
					Arguments: map[string]any{property: call.Input},
				}),
			})
			hasToolCall = true
		default:
			return terminalModelResponse(requestFailedMessage, model.OutcomeFailed), fmt.Errorf(
				"OpenAI Codex returned unsupported output item %q",
				output.Type,
			)
		}
	}
	outcome := defaultOutcome
	if defaultOutcome == model.OutcomeStop && hasToolCall {
		outcome = model.OutcomeToolUse
	}
	responseModel := mo.EmptyableToOption(model.ID(response.Model))
	usage := (model.Usage{
		InputTokens:       response.Usage.InputTokens,
		OutputTokens:      response.Usage.OutputTokens,
		CachedInputTokens: response.Usage.InputTokensDetails.CachedTokens,
		CacheWriteTokens:  response.Usage.InputTokensDetails.CacheWriteTokens,
		ReasoningTokens:   response.Usage.OutputTokensDetails.ReasoningTokens,
		TotalTokens:       response.Usage.TotalTokens,
	}).Normalize()
	responseUsage := mo.None[model.Usage]()
	if response.JSON.Usage.Valid() {
		responseUsage = mo.Some(usage)
	}
	return model.Response{
		Content:       items,
		Outcome:       mo.Some(outcome),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: responseModel,
		ResponseID:    mo.EmptyableToOption(response.ID),
		Usage:         responseUsage,
		Diagnostics:   nil,
	}, nil
}

// modelReasoningContent converts visible summary text and attaches only usable replay context.
func modelReasoningContent(reasoning responses.ResponseReasoningItem) (model.Content, error) {
	summary := lo.Map(reasoning.Summary, func(item responses.ResponseReasoningItemSummary, _ int) string {
		return item.Text
	})
	visible := model.Content{
		Kind:            model.ContentReasoning,
		Text:            mo.Some(strings.Join(summary, "")),
		Final:           true,
		ProviderContext: mo.None[model.ProviderContext](),
		ToolCall:        mo.None[model.ToolCall](),
	}
	if reasoning.ID == "" || reasoning.EncryptedContent == "" {
		return visible, nil
	}
	payload, err := json.Marshal(reasoningContext{
		ID:               reasoning.ID,
		EncryptedContent: reasoning.EncryptedContent,
		Summary:          summary,
	})
	if err != nil {
		return model.Content{}, fmt.Errorf("encode Codex reasoning context: %w", err)
	}
	visible.ProviderContext = mo.Some(model.ProviderContext{
		Source:  model.ProviderContextSource{},
		Payload: payload,
	})
	return visible, nil
}

// failedResponseFromSDK preserves converted finalized items while returning a failed outcome.
func failedResponseFromSDK(
	response responses.Response,
	message string,
	grammarInputProperties map[string]string,
) (model.Response, error) {
	converted, err := modelResponse(response, model.OutcomeFailed, grammarInputProperties)
	if err != nil {
		return terminalModelResponse(message, model.OutcomeFailed), errors.Join(safeError(message), err)
	}
	converted.Outcome = mo.Some(model.OutcomeFailed)
	converted.ErrorMessage = mo.Some(message)
	return converted, safeError(message)
}

// streamError maps cancellation, 401, and bounded provider details without replay.
func (s *Driver) streamError(
	ctx context.Context,
	streamErr error,
	transport *errorCaptureTransport,
) (model.Response, error) {
	if ctx.Err() != nil {
		return terminalModelResponse(requestCanceledMessage, model.OutcomeAborted), ctx.Err()
	}
	if hookFailure, ok := errors.AsType[internalhooks.HookError](streamErr); ok {
		response := hookFailureResponse(hookFailure)
		response.ErrorMessage = mo.Some(streamErr.Error())
		return response, streamErr
	}
	if apiError, ok := errors.AsType[*openai.Error](streamErr); ok {
		if apiError.StatusCode == http.StatusUnauthorized {
			return terminalModelResponse(signInRequiredMessage, model.OutcomeFailed), ErrSignInRequired
		}
		detail := providerErrorDetail([]byte(apiError.RawJSON()))
		if detail == "" {
			detail = providerErrorDetail(transport.ErrorBody())
		}
		if detail == "" {
			detail = boundedDetail(strings.TrimSpace(apiError.Message))
		}
		message := providerFailureMessage(detail)
		return terminalModelResponse(message, model.OutcomeFailed), fmt.Errorf("OpenAI Codex request failed: %w", streamErr)
	}
	failure := fmt.Errorf("OpenAI Codex request failed: %w", streamErr)
	return terminalModelResponse(failure.Error(), model.OutcomeFailed), failure
}

// combineHandlerError adds a handler cause only when the stream error does not already contain it.
func combineHandlerError(streamErr, handlerErr error) error {
	if errors.Is(streamErr, handlerErr) {
		return streamErr
	}
	return errors.Join(streamErr, handlerErr)
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

// boundedErrorMessage returns bounded error text and preserves ErrSignInRequired presentation.
func boundedErrorMessage(err error) string {
	if errors.Is(err, ErrSignInRequired) {
		return signInRequiredMessage
	}
	return boundedDetail(err.Error())
}

// terminalModelResponse creates a payload-free terminal response for a failed or aborted request.
func terminalModelResponse(message string, outcome model.Outcome) model.Response {
	return model.Response{
		Content:       nil,
		Outcome:       mo.Some(outcome),
		ErrorMessage:  mo.Some(message),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
}
