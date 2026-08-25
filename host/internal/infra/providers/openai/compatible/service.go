// Package compatible implements OpenAI-compatible Chat Completions and Responses providers.
package compatible

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

const (
	requestFailedMessage    = "OpenAI-compatible request failed."
	requestCanceledMessage  = "OpenAI-compatible request was canceled."
	credentialFailedMessage = "OpenAI-compatible API key resolution failed." //nolint:gosec // This is an error message.
)

// API identifies one supported OpenAI-compatible wire API.
type API string

const (
	// APIChatCompletions selects the Chat Completions API.
	APIChatCompletions API = "chat-completions"
	// APIResponses selects the Responses API.
	APIResponses API = "responses"

	reasoningWireFormatOpenAIResponses  = "openai-responses"
	reasoningWireFormatOpenAIChatEffort = "openai-chat-effort"
	reasoningWireFormatOllamaOrnith     = "ollama-ornith"
)

// Config contains immutable configuration for one provider instance.
type Config struct {
	ProviderID                 model.ProviderID
	BaseURL                    string
	API                        API
	Models                     map[model.ID]API
	ReasoningWireFormats       map[model.ID]string
	ReasoningCompatibilityKeys map[model.ID]mo.Option[string]
	APIKey                     APIKeyResolver
}

// modelConfig contains provider-owned wire metadata for one configured model.
type modelConfig struct {
	api                       API
	reasoningWireFormat       string
	reasoningCompatibilityKey mo.Option[string]
}

// Driver owns one immutable OpenAI-compatible provider instance.
type Driver struct {
	providerID model.ProviderID
	baseURL    string
	models     map[model.ID]modelConfig
	apiKey     APIKeyResolver
	httpClient *http.Client
}

var _ run.ModelProvider = (*Driver)(nil)

// New validates configuration and creates one provider instance.
func New(config Config) (*Driver, error) {
	if strings.TrimSpace(string(config.ProviderID)) == "" {
		return nil, errors.New("OpenAI-compatible provider ID is required")
	}
	parsedURL, err := url.Parse(config.BaseURL)
	validScheme := parsedURL.Scheme == "http" || parsedURL.Scheme == "https"
	if err != nil || !parsedURL.IsAbs() || !validScheme || parsedURL.Host == "" {
		return nil, errors.New("OpenAI-compatible base URL must be an absolute HTTP or HTTPS URL")
	}
	if apiErr := validateAPI(config.API); apiErr != nil {
		return nil, apiErr
	}
	if len(config.Models) == 0 {
		return nil, errors.New("OpenAI-compatible models are required")
	}
	if config.APIKey == nil {
		return nil, errors.New("OpenAI-compatible API key resolver is required")
	}
	models, err := configuredModels(config)
	if err != nil {
		return nil, err
	}
	return &Driver{
		providerID: config.ProviderID,
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
		models:     models,
		apiKey:     config.APIKey,
		httpClient: &http.Client{},
	}, nil
}

// configuredModels validates and snapshots each model-specific API and reasoning configuration.
func configuredModels(config Config) (map[model.ID]modelConfig, error) {
	return lo.MapEntriesErr(config.Models, func(modelID model.ID, override API) (model.ID, modelConfig, error) {
		var empty modelConfig
		if strings.TrimSpace(string(modelID)) == "" {
			return "", empty, errors.New("OpenAI-compatible model ID is required")
		}
		selectedAPI := config.API
		if override != "" {
			if err := validateAPI(override); err != nil {
				return "", empty, fmt.Errorf("model %q API override: %w", modelID, err)
			}
			selectedAPI = override
		}
		reasoningWireFormat := config.ReasoningWireFormats[modelID]
		if !reasoningWireFormatMatchesAPI(reasoningWireFormat, selectedAPI) {
			return "", empty, fmt.Errorf("model %q reasoning wire format is unsupported for API %q", modelID, selectedAPI)
		}
		return modelID, modelConfig{
			api: selectedAPI, reasoningWireFormat: reasoningWireFormat,
			reasoningCompatibilityKey: config.ReasoningCompatibilityKeys[modelID],
		}, nil
	})
}

func reasoningWireFormatMatchesAPI(format string, api API) bool {
	switch format {
	case "":
		return true
	case reasoningWireFormatOpenAIResponses:
		return api == APIResponses
	case reasoningWireFormatOpenAIChatEffort, reasoningWireFormatOllamaOrnith:
		return api == APIChatCompletions
	default:
		return false
	}
}

func validateAPI(api API) error {
	if api != APIChatCompletions && api != APIResponses {
		return fmt.Errorf("unsupported OpenAI-compatible API %q", api)
	}
	return nil
}

// Stream emits one provider response as provider-neutral events.
//
//nolint:nestif // Error classification must preserve handler, cancellation, and provider outcomes.
func (s *Driver) Stream(ctx context.Context, request run.ModelRequest, handle run.StreamHandler) error {
	configuredModel, err := s.requestModelConfig(request)
	if err != nil {
		return s.emitFailure(handle, request, model.OutcomeFailed, requestFailedMessage, err)
	}
	key, err := s.apiKey.ResolveAPIKey(ctx)
	if err != nil {
		return s.emitFailure(
			handle, request, model.OutcomeFailed, credentialFailedMessage,
			errors.New("API key resolution failed"),
		)
	}
	var response model.Response
	switch configuredModel.api {
	case APIChatCompletions:
		response, err = s.streamChatCompletions(ctx, request, configuredModel, key, handle)
	case APIResponses:
		response, err = s.streamResponses(ctx, request, key, handle)
	}
	if err != nil {
		var deliveryFailure streamHandlerError
		if errors.As(err, &deliveryFailure) {
			return deliveryFailure.err
		}
		outcome := model.OutcomeFailed
		message := requestFailedMessage
		if ctx.Err() != nil {
			outcome = model.OutcomeAborted
			message = requestCanceledMessage
			err = ctx.Err()
			if err == nil {
				err = errors.New("request canceled")
			}
		} else {
			err = errors.New(strings.TrimSuffix(message, "."))
		}
		if response.Outcome.OrEmpty() == 0 {
			response = failureResponse(outcome, message)
		}
		response.Provider = mo.Some(s.providerID)
		response.Model = mo.Some(request.Model.Model)
		if handleErr := handle(run.StreamEvent{
			Kind:     run.StreamEventError,
			Position: 0,
			Content:  model.Content{},
			Delta:    "",
			Preview:  model.ToolCallPreview{},
			ToolCall: model.ToolCall{},
			Response: response,
		}); handleErr != nil {
			return handleErr
		}
		return err
	}
	response.Provider = mo.Some(s.providerID)
	response.Model = mo.Some(request.Model.Model)
	if handleErr := handle(run.StreamEvent{
		Kind:     run.StreamEventDone,
		Position: 0,
		Content:  model.Content{},
		Delta:    "",
		Preview:  model.ToolCallPreview{},
		ToolCall: model.ToolCall{},
		Response: response,
	}); handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Driver) requestModelConfig(request run.ModelRequest) (modelConfig, error) {
	if request.Model.Provider != s.providerID {
		return modelConfig{}, errors.New("configured provider does not match request")
	}
	configuredModel, ok := s.models[request.Model.Model]
	if !ok {
		return modelConfig{}, errors.New("configured model does not match request")
	}
	return configuredModel, nil
}

func (s *Driver) emitFailure(
	handle run.StreamHandler,
	request run.ModelRequest,
	outcome model.Outcome,
	message string,
	err error,
) error {
	response := failureResponse(outcome, message)
	response.Provider = mo.Some(s.providerID)
	response.Model = mo.Some(request.Model.Model)
	if handleErr := handle(run.StreamEvent{
		Kind:     run.StreamEventError,
		Position: 0,
		Content:  model.Content{},
		Delta:    "",
		Preview:  model.ToolCallPreview{},
		ToolCall: model.ToolCall{},
		Response: response,
	}); handleErr != nil {
		return handleErr
	}
	return err
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

type streamHandlerError struct{ err error }

func (failure streamHandlerError) Error() string {
	return "stream handler failed: " + failure.err.Error()
}
func (failure streamHandlerError) Unwrap() error { return failure.err }

func handlerError(err error) error { return streamHandlerError{err: err} }
