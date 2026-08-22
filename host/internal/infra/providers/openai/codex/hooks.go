package codex

import (
	"bytes"
	"io"
	"net/http"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	internalhooks "github.com/n-r-w/glyph/host/internal/hooks"
)

// hookTransport runs copied request and response values at the HTTP boundary.
type hookTransport struct {
	base     http.RoundTripper
	runner   internalhooks.ProviderRunner
	provider model.ProviderID
	model    model.ID
}

var _ http.RoundTripper = (*hookTransport)(nil)

// RoundTrip transforms the serialized request and observes response metadata before body decoding.
func (transport *hookTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	payload, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if closeErr := request.Body.Close(); closeErr != nil {
		return nil, closeErr
	}
	transformed, err := transport.runner.TransformRequest(request.Context(), internalhooks.Request{
		Provider: transport.provider,
		Model:    transport.model,
		Payload:  append([]byte(nil), payload...),
		Headers:  headerCopy(request.Header),
	})
	if err != nil {
		return nil, internalhooks.HookError{Stage: internalhooks.StageRequest}
	}

	dispatched := request.Clone(request.Context())
	dispatched.Header = http.Header(transformed.Headers).Clone()
	dispatched.Body = io.NopCloser(bytes.NewReader(transformed.Payload))
	dispatched.ContentLength = int64(len(transformed.Payload))
	dispatched.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(transformed.Payload)), nil
	}
	response, err := transport.base.RoundTrip(dispatched)
	if err != nil {
		return response, err
	}
	if observeErr := transport.runner.ObserveResponse(request.Context(), internalhooks.Response{
		Provider: transport.provider,
		Model:    transport.model,
		Status:   response.StatusCode,
		Headers:  headerCopy(response.Header),
	}); observeErr != nil {
		_ = response.Body.Close()
		return nil, internalhooks.HookError{Stage: internalhooks.StageResponse}
	}
	return response, nil
}

func headerCopy(header http.Header) internalhooks.Header {
	if header == nil {
		return nil
	}
	cloned := make(internalhooks.Header, len(header))
	for name, values := range header {
		cloned[name] = append([]string(nil), values...)
	}
	return cloned
}

func hookFailureResponse(stage internalhooks.Stage) model.Response {
	response := emptyModelResponse()
	response.Outcome = model.OutcomeFailed
	response.ErrorMessage = "Model request failed."
	response.Diagnostics = []model.Diagnostic{{Code: "internal_hook_failed", Message: string(stage)}}
	return response
}
