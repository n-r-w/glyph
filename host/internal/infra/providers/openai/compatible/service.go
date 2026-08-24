// Package compatible implements OpenAI-compatible Chat Completions and Responses providers.
//
//nolint:exhaustruct // Provider mappings set only fields supported by Agent Core.
package compatible

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
)

// Config contains immutable configuration for one provider instance.
type Config struct {
	ProviderID model.ProviderID
	BaseURL    string
	API        API
	Models     map[model.ID]API
	APIKey     APIKeyResolver
}

// Service owns one immutable OpenAI-compatible provider instance.
type Service struct {
	providerID model.ProviderID
	baseURL    string
	api        API
	models     map[model.ID]API
	apiKey     APIKeyResolver
	httpClient *http.Client
}

var _ run.ModelProvider = (*Service)(nil)

// New validates configuration and creates one provider instance.
func New(config Config) (*Service, error) {
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
	models := make(map[model.ID]API, len(config.Models))
	for modelID, override := range config.Models {
		if strings.TrimSpace(string(modelID)) == "" {
			return nil, errors.New("OpenAI-compatible model ID is required")
		}
		if override != "" {
			if apiErr := validateAPI(override); apiErr != nil {
				return nil, fmt.Errorf("model %q API override: %w", modelID, apiErr)
			}
		}
		models[modelID] = override
	}
	return &Service{
		providerID: config.ProviderID,
		baseURL:    strings.TrimRight(config.BaseURL, "/"),
		api:        config.API,
		models:     models,
		apiKey:     config.APIKey,
		httpClient: &http.Client{},
	}, nil
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
func (s *Service) Stream(ctx context.Context, request run.ModelRequest, handle run.StreamHandler) error {
	selectedAPI, err := s.requestAPI(request)
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
	switch selectedAPI {
	case APIChatCompletions:
		response, err = s.streamChatCompletions(ctx, request, key, handle)
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
		if response.Outcome == 0 {
			response = failureResponse(outcome, message)
		}
		response.Provider = s.providerID
		response.Model = request.Model.Model
		if handleErr := handle(run.StreamEvent{Kind: run.StreamEventError, Response: response}); handleErr != nil {
			return handleErr
		}
		return err
	}
	response.Provider = s.providerID
	response.Model = request.Model.Model
	if handleErr := handle(run.StreamEvent{Kind: run.StreamEventDone, Response: response}); handleErr != nil {
		return handleErr
	}
	return nil
}

func (s *Service) requestAPI(request run.ModelRequest) (API, error) {
	if request.Model.Provider != s.providerID {
		return "", errors.New("configured provider does not match request")
	}
	override, ok := s.models[request.Model.Model]
	if !ok || request.Model.Model == "" {
		return "", errors.New("configured model does not match request")
	}
	if override != "" {
		return override, nil
	}
	return s.api, nil
}

func (s *Service) emitFailure(
	handle run.StreamHandler,
	request run.ModelRequest,
	outcome model.Outcome,
	message string,
	err error,
) error {
	response := failureResponse(outcome, message)
	response.Provider = s.providerID
	response.Model = request.Model.Model
	if handleErr := handle(run.StreamEvent{Kind: run.StreamEventError, Response: response}); handleErr != nil {
		return handleErr
	}
	return err
}

func failureResponse(outcome model.Outcome, message string) model.Response {
	return model.Response{Outcome: outcome, ErrorMessage: message}
}

type streamHandlerError struct{ err error }

func (failure streamHandlerError) Error() string {
	return "stream handler failed: " + failure.err.Error()
}
func (failure streamHandlerError) Unwrap() error { return failure.err }

func handlerError(err error) error { return streamHandlerError{err: err} }
