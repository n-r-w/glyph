package openai

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

const (
	// redactedWireValue replaces known private header values in public observations and errors.
	redactedWireValue = "[REDACTED]"
	// wireAuthorizationHeader identifies bearer credentials.
	wireAuthorizationHeader = "authorization"
	// wireProxyAuthorizationHeader identifies proxy credentials.
	wireProxyAuthorizationHeader = "proxy-authorization"
	// wireProxyAuthenticateHeader identifies proxy authentication state.
	wireProxyAuthenticateHeader = "proxy-authenticate"
	// wireAuthenticateHeader identifies provider authentication challenges.
	wireAuthenticateHeader = "www-authenticate"
	// wireCookieHeader identifies request cookies.
	wireCookieHeader = "cookie"
	// wireSetCookieHeader identifies response cookies.
	wireSetCookieHeader = "set-cookie"
	// wireAccountHeader identifies Codex account ownership.
	wireAccountHeader = "chatgpt-account-id"
	// wireOrganizationHeader identifies OpenAI account ownership.
	wireOrganizationHeader = "openai-organization"
	// wireProjectHeader identifies OpenAI project ownership.
	wireProjectHeader = "openai-project"
	// wireXAPIKeyHeader identifies common compatible-provider credentials.
	wireXAPIKeyHeader = "x-api-key"
	// wireAPIKeyHeader identifies common compatible-provider credentials.
	wireAPIKeyHeader = "api-key"
	// wireHeaderPunctuation contains the punctuation allowed in an HTTP field-name token.
	wireHeaderPunctuation = "!#$%&'*+-.^_`|~"
)

// acquireWireAuthentication resolves URL authentication before projection and acquires password representations.
func acquireWireAuthentication(request *http.Request) []string {
	if request.URL.User == nil {
		return nil
	}
	user := request.URL.User
	password, _ := user.Password()
	if request.Header.Get(wireAuthorizationHeader) == "" {
		request.SetBasicAuth(user.Username(), password)
	}
	_, encodedPassword, _ := strings.Cut(user.String(), ":")
	return []string{password, encodedPassword}
}

// prepareWireRequest applies validated public values while retaining private authentication and replay.
func prepareWireRequest(request *http.Request, callbacks modelexecution.WireCallbacks, secrets []string) error {
	headers, err := callbacks.Headers(
		request.Context(),
		projectPublicWireHeaders(request.Header, secrets),
		func(headers modelexecution.WireHeaders) error {
			return replaceWireHeaders(request.Header.Clone(), headers)
		},
	)
	if err != nil {
		return err
	}
	if headerErr := replaceWireHeaders(request.Header, headers); headerErr != nil {
		return headerErr
	}
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return fmt.Errorf("read serialized provider request: %w", err)
	}
	if closeErr := request.Body.Close(); closeErr != nil {
		return fmt.Errorf("close serialized provider request: %w", closeErr)
	}
	projection, safe, err := projectWireBody(raw)
	if err != nil {
		return err
	}
	result, err := callbacks.Body(request.Context(), safe, func(body []byte) error {
		_, validationErr := projection.restore(body)
		return validationErr
	})
	if err != nil {
		return err
	}
	if result.Replaced {
		raw, err = projection.restore(result.Body)
		if err != nil {
			return err
		}
	}
	request.Body = io.NopCloser(bytes.NewReader(raw))
	request.ContentLength = int64(len(raw))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	return nil
}

// wireError redacts known private values while preserving source identity for internal decisions.
type wireError struct {
	// cause retains the complete acquired source identity.
	cause error
	// secrets contains known authentication values acquired during the attempt.
	secrets []string
}

// Verify the diagnostic envelope implements the standard error contract.
var _ error = (*wireError)(nil)

// Error exposes complete non-secret middleware failure text.
func (err *wireError) Error() string { return RedactWireText(err.cause.Error(), err.secrets) }

// Unwrap retains source identity without substituting provider classification for middleware meaning.
func (err *wireError) Unwrap() error { return err.cause }

// isProtectedWireHeader identifies authentication, cookie and account state owned by the provider.
func isProtectedWireHeader(name string) bool {
	switch strings.ToLower(name) {
	case wireAuthorizationHeader, wireProxyAuthorizationHeader, wireProxyAuthenticateHeader, wireAuthenticateHeader,
		wireCookieHeader, wireSetCookieHeader, wireAccountHeader, wireOrganizationHeader, wireProjectHeader,
		wireXAPIKeyHeader, wireAPIKeyHeader:
		return true
	default:
		return false
	}
}

// CollectWireSecrets acquires authentication secrets while retaining ordinary identifiers in diagnostics.
func CollectWireSecrets(headers http.Header) []string {
	var secrets []string
	for name, values := range headers {
		switch strings.ToLower(name) {
		case wireAuthorizationHeader, wireProxyAuthorizationHeader, wireCookieHeader, wireSetCookieHeader,
			wireXAPIKeyHeader, wireAPIKeyHeader:
		default:
			continue
		}
		for _, value := range values {
			if value == "" {
				continue
			}
			if strings.EqualFold(name, wireCookieHeader) || strings.EqualFold(name, wireSetCookieHeader) {
				secrets = append(secrets, collectWireCookieValues(name, value)...)
				continue
			}
			secrets = append(secrets, value)
			if _, token, found := strings.Cut(value, " "); found && token != "" {
				secrets = append(secrets, token)
			}
		}
	}
	return secrets
}

// collectWireCookieValues acquires cookie values without treating Set-Cookie attributes as private credentials.
func collectWireCookieValues(name, value string) []string {
	parts := strings.Split(value, ";")
	if strings.EqualFold(name, wireSetCookieHeader) {
		parts = parts[:1]
	}
	var values []string
	for _, part := range parts {
		_, cookie, found := strings.Cut(part, "=")
		cookie = strings.Trim(strings.TrimSpace(cookie), "\"")
		if found && cookie != "" {
			values = append(values, cookie)
		}
	}
	return values
}

// RedactWireText replaces supplied nonempty secret strings without interpreting surrounding diagnostic text.
func RedactWireText(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, redactedWireValue)
		}
	}
	return text
}

// projectPublicWireHeaders detaches public values and redacts known authentication echoes.
func projectPublicWireHeaders(headers http.Header, secrets []string) modelexecution.WireHeaders {
	result := make(modelexecution.WireHeaders)
	for name, values := range headers {
		if isProtectedWireHeader(name) {
			continue
		}
		for _, value := range values {
			result[name] = append(result[name], RedactWireText(value, secrets))
		}
	}
	return result
}

// replaceWireHeaders validates public replacement before changing the authenticated request.
func replaceWireHeaders(headers http.Header, replacement modelexecution.WireHeaders) error {
	normalized := make(http.Header, len(replacement))
	for name, values := range replacement {
		if isProtectedWireHeader(name) {
			return fmt.Errorf("middleware cannot write protected header %q", name)
		}
		if !isValidWireHeaderName(name) {
			return fmt.Errorf("middleware returned invalid header name %q", name)
		}
		canonical := http.CanonicalHeaderKey(name)
		if _, exists := normalized[canonical]; exists {
			return fmt.Errorf("middleware duplicated header %q", canonical)
		}
		for _, value := range values {
			if !isValidWireHeaderValue(value) {
				return fmt.Errorf("middleware returned invalid value for header %q", name)
			}
		}
		normalized[canonical] = append([]string(nil), values...)
	}
	for name := range headers {
		if !isProtectedWireHeader(name) {
			delete(headers, name)
		}
	}
	maps.Copy(headers, normalized)
	return nil
}

// isValidWireHeaderValue rejects HTTP control bytes except horizontal tab before dispatch.
func isValidWireHeaderValue(value string) bool {
	for i := range len(value) {
		if value[i] == '\t' {
			continue
		}
		if value[i] < 0x20 || value[i] == 0x7f {
			return false
		}
	}
	return true
}

// isValidWireHeaderName checks the HTTP token grammar at the serialized request boundary.
func isValidWireHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune(wireHeaderPunctuation, r) {
			continue
		}
		return false
	}
	return true
}
