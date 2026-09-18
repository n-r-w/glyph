// Package compatible implements OpenAI-compatible Chat Completions and Responses providers.
package compatible

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/errtree"
	providerconsts "github.com/n-r-w/glyph/host/internal/infra/providers"
	openaifailure "github.com/n-r-w/glyph/host/internal/infra/providers/openai"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

const (
	requestFailedMessage   = "OpenAI-compatible request failed."
	requestCanceledMessage = "OpenAI-compatible request was canceled."
	// chatMissingTerminalMessage identifies a cleanly closed Chat Completions stream.
	chatMissingTerminalMessage = "chat completions stream ended without a finish reason"
	// responsesMissingTerminalMessage identifies a cleanly closed Responses stream.
	responsesMissingTerminalMessage = "responses stream ended without a terminal response"
)

var (
	// errChatMissingTerminal identifies a cleanly closed Chat Completions stream without a terminal outcome.
	errChatMissingTerminal = errors.New(chatMissingTerminalMessage)
	// errResponsesMissingTerminal identifies a cleanly closed Responses stream without a terminal outcome.
	errResponsesMissingTerminal = errors.New(responsesMissingTerminalMessage)
)

// API identifies one supported OpenAI-compatible wire API.
type API string

const (
	// APIChatCompletions selects the Chat Completions API.
	APIChatCompletions API = "chat-completions"
	// APIResponses selects the Responses API.
	APIResponses API = "responses"
)

// Config contains immutable configuration for one provider instance.
type Config struct {
	// ProviderID identifies the configured provider instance.
	ProviderID model.ProviderID
	// BaseURL is the provider API endpoint.
	BaseURL string
	// API identifies the default provider request contract.
	API API
	// Models contains request contract overrides by model.
	Models map[model.ID]API
	// ReasoningFormats contains raw formats for models with reasoning enabled.
	ReasoningFormats map[model.ID]string
	// ReasoningCompatibilityKeys contains replay contracts by model.
	ReasoningCompatibilityKeys map[model.ID]mo.Option[string]
	// APIKey resolves the provider credential for each request.
	APIKey APIKeyResolver
}

// modelConfig contains provider-owned wire metadata for one configured model.
type modelConfig struct {
	// api identifies the provider request contract.
	api API
	// reasoningFormat identifies the reasoning request format.
	reasoningFormat reasoningFormat
	// reasoningCompatibilityKey identifies the replay contract.
	reasoningCompatibilityKey mo.Option[string]
}

// Driver owns one immutable OpenAI-compatible provider instance.
type Driver struct {
	// providerID identifies the configured provider instance.
	providerID model.ProviderID
	// baseURL is the provider API endpoint.
	baseURL string
	// models contains provider wire metadata by model.
	models map[model.ID]modelConfig
	// apiKey resolves the provider credential for each request.
	apiKey APIKeyResolver
	// httpClient sends provider requests.
	httpClient *http.Client
	// headers contains provider identification and attribution headers.
	headers map[string]string
}

var _ modelexecution.ProviderAttempt = (*Driver)(nil)

// New validates configuration and creates one provider instance.
func New(config Config) (*Driver, error) {
	if strings.TrimSpace(string(config.ProviderID)) == "" {
		return nil, errors.New("OpenAI-compatible provider ID is required")
	}
	parsedURL, err := url.Parse(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAI-compatible base URL: %w", err)
	}
	validScheme := parsedURL.Scheme == "http" || parsedURL.Scheme == "https"
	if !parsedURL.IsAbs() || !validScheme || parsedURL.Host == "" {
		return nil, errors.New("OpenAI-compatible base URL must be an absolute HTTP or HTTPS URL")
	}
	if apiErr := config.API.Validate(); apiErr != nil {
		return nil, apiErr
	}
	if len(config.Models) == 0 {
		return nil, errors.New("OpenAI-compatible models are required")
	}
	if config.APIKey == nil {
		return nil, errors.New("OpenAI-compatible API key resolver is required")
	}
	models, err := config.configuredModels()
	if err != nil {
		return nil, err
	}
	return &Driver{
		providerID: config.ProviderID,
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
		models:     models,
		apiKey:     config.APIKey,
		httpClient: &http.Client{},
		headers:    requestHeaders(config.ProviderID, parsedURL),
	}, nil
}

// requestHeaders builds identification and attribution headers for one provider.
func requestHeaders(providerID model.ProviderID, baseURL *url.URL) map[string]string {
	headers := map[string]string{"User-Agent": providerconsts.AgentID}
	if providerID == "openrouter" || baseURL.Hostname() == "openrouter.ai" {
		headers["HTTP-Referer"] = providerconsts.ProjectURL
		headers["X-OpenRouter-Title"] = providerconsts.AgentTitle
		headers["X-OpenRouter-Categories"] = "cli-agent"
	}
	return headers
}

// requestOptions builds the shared OpenAI SDK options for one provider request.
func (s *Driver) requestOptions(key string) []option.RequestOption {
	opts := []option.RequestOption{
		option.WithBaseURL(s.baseURL),
		option.WithHTTPClient(s.httpClient),
		option.WithMaxRetries(0),
	}
	for name, value := range s.headers {
		opts = append(opts, option.WithHeader(name, value))
	}
	if key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}
	return opts
}

// configuredModels validates and snapshots each model-specific API and reasoning configuration.
func (config Config) configuredModels() (map[model.ID]modelConfig, error) {
	return lo.MapEntriesErr(config.Models, func(modelID model.ID, override API) (model.ID, modelConfig, error) {
		if strings.TrimSpace(string(modelID)) == "" {
			return "", modelConfig{}, errors.New("OpenAI-compatible model ID is required")
		}
		selectedAPI := config.API
		if override != "" {
			if err := override.Validate(); err != nil {
				return "", modelConfig{}, fmt.Errorf("model %q API override: %w", modelID, err)
			}
			selectedAPI = override
		}
		rawFormat, reasoningConfigured := config.ReasoningFormats[modelID]
		parsedFormat, err := parseReasoningFormat(rawFormat, selectedAPI, reasoningConfigured)
		if err != nil {
			return "", modelConfig{}, fmt.Errorf("provider %q model %q: %w", config.ProviderID, modelID, err)
		}
		return modelID, modelConfig{
			api: selectedAPI, reasoningFormat: parsedFormat,
			reasoningCompatibilityKey: config.ReasoningCompatibilityKeys[modelID],
		}, nil
	})
}

// Validate checks whether the API belongs to the supported closed set.
func (api API) Validate() error {
	if api != APIChatCompletions && api != APIResponses {
		return fmt.Errorf("unsupported OpenAI-compatible API %q", api)
	}
	return nil
}

// Stream emits one provider response as provider-neutral events.
func (s *Driver) Stream(
	ctx context.Context,
	request modelexecution.ProviderRequest,
	handle modelexecution.StreamHandler,
) error {
	configuredModel, err := s.modelConfigForRequest(request)
	if err != nil {
		return s.emitFailure(handle, request, err.Error(), err)
	}
	key, err := s.apiKey.ResolveAPIKey(ctx)
	if err != nil {
		credentialErr := fmt.Errorf("resolve OpenAI-compatible API key: %w", err)
		return s.emitFailure(handle, request, credentialErr.Error(), credentialErr)
	}
	target := s.providerContextTarget(request, configuredModel)
	var response model.Response
	switch configuredModel.api {
	case APIChatCompletions:
		params, prepareErr := chatParams(request, configuredModel.reasoningFormat, target)
		if prepareErr != nil {
			return s.emitFailure(handle, request, prepareErr.Error(), prepareErr)
		}
		response, err = s.streamChatCompletions(ctx, configuredModel, key, params, target, handle)
	case APIResponses:
		params, prepareErr := responsesParams(request, target)
		if prepareErr != nil {
			return s.emitFailure(handle, request, prepareErr.Error(), prepareErr)
		}
		response, err = s.streamResponses(ctx, key, params, target, handle)
	}
	if err != nil {
		return s.completeProviderFailure(ctx, request, response, err, handle)
	}
	response.Provider = mo.Some(s.providerID)
	response.Model = mo.Some(request.Model.Model)
	if handleErr := handle(terminalStreamEvent(modelexecution.StreamEventDone, response)); handleErr != nil {
		return handleErr
	}
	return nil
}

// completeProviderFailure delivers one model-dispatch failure and attaches provider source metadata.
func (s *Driver) completeProviderFailure(
	ctx context.Context,
	request modelexecution.ProviderRequest,
	response model.Response,
	sourceErr error,
	handle modelexecution.StreamHandler,
) error {
	if _, deliveryFailure := errors.AsType[streamHandlerError](sourceErr); deliveryFailure {
		return sourceErr
	}
	outcome := model.OutcomeFailed
	providerErr := fmt.Errorf("OpenAI-compatible request failed: %w", sourceErr)
	message := providerErr.Error()
	var resultErr error
	if ctx.Err() != nil {
		outcome = model.OutcomeAborted
		if isPureCancellation(sourceErr) {
			message = requestCanceledMessage
			resultErr = ctx.Err()
		} else {
			resultErr = errors.Join(ctx.Err(), providerErr)
		}
	} else {
		resultErr = providerErr
	}
	if responseOutcome, present := response.Outcome.Get(); !present || responseOutcome == 0 {
		response = failureResponse(outcome, message)
	} else {
		response.Outcome = mo.Some(outcome)
		response.ErrorMessage = mo.Some(message)
	}
	response.Provider = mo.Some(s.providerID)
	response.Model = mo.Some(request.Model.Model)
	if handleErr := handle(terminalStreamEvent(modelexecution.StreamEventError, response)); handleErr != nil {
		return combineFinalHandlerError(resultErr, handleErr)
	}
	if ctx.Err() != nil && isPureCancellation(sourceErr) {
		return resultErr
	}
	return classifyProviderFailure(sourceErr, resultErr)
}

// providerContextTarget returns the exact replay identity for one prepared request.
func (s *Driver) providerContextTarget(
	request modelexecution.ProviderRequest,
	configured modelConfig,
) model.ProviderContextSource {
	return model.ProviderContextSource{
		ProviderID:       s.providerID,
		API:              string(configured.api),
		Model:            request.Model.Model,
		CompatibilityKey: configured.reasoningCompatibilityKey,
	}
}

// modelConfigForRequest returns provider wire settings for the selected model.
func (s *Driver) modelConfigForRequest(request modelexecution.ProviderRequest) (modelConfig, error) {
	if request.Model.Provider != s.providerID {
		return modelConfig{}, errors.New("configured provider does not match request")
	}
	configuredModel, ok := s.models[request.Model.Model]
	if !ok {
		return modelConfig{}, errors.New("configured model does not match request")
	}
	return configuredModel, nil
}

// emitFailure reports a pre-dispatch failure and retains any terminal-delivery cause.
func (s *Driver) emitFailure(
	handle modelexecution.StreamHandler,
	request modelexecution.ProviderRequest,
	message string,
	err error,
) error {
	response := failureResponse(model.OutcomeFailed, message)
	response.Provider = mo.Some(s.providerID)
	response.Model = mo.Some(request.Model.Model)
	if handleErr := handle(terminalStreamEvent(modelexecution.StreamEventError, response)); handleErr != nil {
		return combineFinalHandlerError(err, handleErr)
	}
	return err
}

// classifyProviderFailure attaches source-owned metadata after successful terminal delivery.
func classifyProviderFailure(sourceErr, cause error) error {
	var classification modelexecution.ProviderFailureClassification
	retryDelay := mo.None[time.Duration]()
	if apiError, hasAPIError := errors.AsType[*openai.Error](sourceErr); hasAPIError {
		classification = openaifailure.FailureClassification(apiError.Code, apiError.StatusCode, false)
		retryDelay = openaifailure.RetryAfterDelay(apiError.Response)
	} else if responseError, hasResponseError := errors.AsType[*providerResponseError](sourceErr); hasResponseError {
		classification = openaifailure.FailureClassification(responseError.code, 0, false)
	} else {
		incomplete := errors.Is(sourceErr, errChatMissingTerminal) ||
			errors.Is(sourceErr, errResponsesMissingTerminal)
		classification = openaifailure.FailureClassification(
			"", 0, incomplete || openaifailure.IsTransientTransportFailure(sourceErr),
		)
	}
	return &modelexecution.ProviderFailureError{
		Classification: classification,
		RetryDelay:     retryDelay,
		Cause:          cause,
	}
}

func failureResponse(outcome model.Outcome, message string) model.Response {
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

// isPureCancellation reports whether every leaf is a cancellation marker recognized by the driver.
func isPureCancellation(err error) bool {
	return errtree.AllLeavesMatch(err, func(cause error) bool {
		return errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded)
	})
}

type streamHandlerError struct {
	// err contains the retained stream handler failure.
	err error
}

func (failure streamHandlerError) Error() string {
	return "stream handler failed: " + failure.err.Error()
}
func (failure streamHandlerError) Unwrap() error { return failure.err }

func handlerError(err error) error { return streamHandlerError{err: err} }

// combineFinalHandlerError retains the terminal source error and tags the delivery cause once.
func combineFinalHandlerError(sourceErr, handleErr error) error {
	if errors.Is(sourceErr, handleErr) {
		return sourceErr
	}
	return errors.Join(sourceErr, handlerError(handleErr))
}

// tagHandlerErrors distinguishes delivery failures from provider adapter failures.
func tagHandlerErrors(handle modelexecution.StreamHandler) modelexecution.StreamHandler {
	return func(event modelexecution.StreamEvent) error {
		handleErr := handle(event)
		if handleErr != nil {
			return handlerError(handleErr)
		}
		return nil
	}
}
