package openai

import (
	"errors"
	"net/http"

	"github.com/openai/openai-go/v3/option"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// WireDiagnostics retains known authentication secrets for one raw provider attempt through final completion.
type WireDiagnostics struct {
	// secrets contains acquired request and response credentials for diagnostic masking.
	secrets []string
}

// NewWireOption maps callbacks into the SDK boundary and retains secrets for final RedactError and RedactText calls.
func (diagnostics *WireDiagnostics) NewWireOption(
	callbacks mo.Option[modelexecution.WireCallbacks],
) option.RequestOption {
	value, present := callbacks.Get()
	return option.WithMiddleware(func(request *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		diagnostics.secrets = append(diagnostics.secrets, acquireWireAuthentication(request)...)
		diagnostics.CaptureHeaders(request.Header)
		fail := func(err error) error {
			safe := diagnostics.RedactError(err)
			if value.Failed != nil {
				value.Failed(safe)
			}
			return safe
		}
		if present {
			if err := prepareWireRequest(request, value, diagnostics.secrets); err != nil {
				return nil, fail(err)
			}
		}
		response, providerErr := next(request)
		if response == nil {
			return nil, diagnostics.RedactError(providerErr)
		}
		diagnostics.CaptureHeaders(response.Header)
		if !present {
			return response, diagnostics.RedactError(providerErr)
		}
		observerErr := value.Response(
			request.Context(),
			modelexecution.WireResponse{
				Status:  int64(response.StatusCode),
				Headers: projectPublicWireHeaders(response.Header, diagnostics.secrets),
			},
		)
		if observerErr != nil {
			var closeErr error
			if response.Body != nil {
				closeErr = response.Body.Close()
			}
			return nil, fail(errors.Join(observerErr, providerErr, closeErr))
		}
		return response, diagnostics.RedactError(providerErr)
	})
}

// CaptureHeaders retains credential values from privately acquired metadata for final diagnostics.
func (diagnostics *WireDiagnostics) CaptureHeaders(headers http.Header) {
	diagnostics.secrets = append(diagnostics.secrets, CollectWireSecrets(headers)...)
}

// RedactError preserves source identities while masking the attempt's acquired authentication secrets.
func (diagnostics *WireDiagnostics) RedactError(err error) error {
	if err == nil {
		return nil
	}
	return &wireError{cause: err, secrets: diagnostics.secrets}
}

// RedactText masks acquired authentication secrets in a completed diagnostic message.
func (diagnostics *WireDiagnostics) RedactText(text string) string {
	return RedactWireText(text, diagnostics.secrets)
}
