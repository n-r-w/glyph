//go:build integration

package app

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// queueRejectedRequest is the scripted response that makes the provider reject the request with a failure that Host
// does not retry.
const queueRejectedRequest = "rejected request"

// queueProvider is a local Responses endpoint that records request bodies and holds its first requests.
type queueProvider struct {
	// server serves the scripted responses.
	server *httptest.Server
	// held contains one channel per held request, closed when that request arrives.
	held []chan struct{}
	// release contains one channel per held request; closing it lets that request return its response.
	release []chan struct{}
	// mutex protects bodies.
	mutex sync.Mutex
	// bodies contains each request body in arrival order.
	bodies []string
}

// startQueueProvider serves the responses in order; each of the first heldRequests requests waits until the test
// releases it, and the response queueRejectedRequest makes the provider reject its request with a failure that
// Host does not retry.
func startQueueProvider(t *testing.T, heldRequests int, responses ...string) *queueProvider {
	t.Helper()
	provider := &queueProvider{
		server: nil, held: make([]chan struct{}, heldRequests), release: make([]chan struct{}, heldRequests),
		mutex: sync.Mutex{}, bodies: nil,
	}
	for index := range heldRequests {
		provider.held[index] = make(chan struct{})
		provider.release[index] = make(chan struct{})
	}
	provider.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		assert.NoError(t, err)
		provider.mutex.Lock()
		provider.bodies = append(provider.bodies, string(body))
		position := len(provider.bodies)
		provider.mutex.Unlock()
		if position > len(responses) {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		if position <= heldRequests {
			close(provider.held[position-1])
			select {
			case <-provider.release[position-1]:
			case <-request.Context().Done():
				return
			}
		}
		if responses[position-1] == queueRejectedRequest {
			writer.Header().Set(retryContentTypeHeader, retryJSONContentType)
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(retryProviderError))
			return
		}
		writer.Header().Set(retryContentTypeHeader, retryStreamContentType)
		_, _ = writer.Write([]byte(responses[position-1]))
	}))
	t.Cleanup(provider.server.Close)
	return provider
}

// snapshot returns the recorded request bodies.
func (provider *queueProvider) snapshot() []string {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]string(nil), provider.bodies...)
}

// queueSettings selects a Responses-compatible provider at the given endpoint.
func queueSettings(serverURL string) string {
	compatible := strings.Replace(
		strings.SplitN(configuredRequestSettings, retryProvidersKey, 2)[1],
		retryCodexProviderKey,
		retryLocalProviderKey,
		1,
	)
	compatible = strings.Replace(
		compatible, retryCodexProviderType, fmt.Sprintf(retryCompatibleProviderFormat, serverURL), 1,
	)
	settings := strings.Replace(configuredRequestSettings, retryCodexDefault, retryLocalDefault, 1)
	return strings.Replace(settings, retryProvidersKey, retryProvidersKey+compatible, 1)
}

// startQueueFixture starts a Programmatic Control process against the provider.
func startQueueFixture(t *testing.T, provider *queueProvider, extensionDirectory string) *programmaticFixture {
	t.Helper()
	paths := testPaths(t, queueSettings(provider.server.URL))
	writeProgrammaticCredentials(t, paths)
	return startProgrammaticFixtureWithExtension(t, paths, extensionDirectory)
}
