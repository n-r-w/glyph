//go:build integration

package compatible

import (
	"net/http"
	"net/http/httptest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

func (s *serviceSuite) TestAPIKeyResolvesBeforeEveryRequest() {
	t := s.T()
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		writer.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, writer, `{"id":"chat-key","choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(server.Close)
	resolver := NewMockAPIKeyResolver(gomock.NewController(t))
	gomock.InOrder(
		resolver.EXPECT().ResolveAPIKey(gomock.Any()).Return("first-key", nil),
		resolver.EXPECT().ResolveAPIKey(gomock.Any()).Return("second-key", nil),
	)
	service, err := New(Config{
		ProviderID: "local", BaseURL: server.URL, API: APIChatCompletions,
		Models: map[model.ID]API{"demo": ""}, APIKey: resolver, ReasoningFormats: nil, ReasoningCompatibilityKeys: nil,
	})
	require.NoError(t, err)

	streamEvents(t, service, richRequest("local", "demo"))
	streamEvents(t, service, richRequest("local", "demo"))

	assert.Equal(t, []string{"Bearer first-key", "Bearer second-key"}, authorizations)
}
