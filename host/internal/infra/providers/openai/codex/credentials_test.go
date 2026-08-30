//go:build integration

package codex

import (
	"encoding/json/v2"

	"fmt"

	"net/http"
	"net/http/httptest"

	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"

	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestDriverStreamRefreshesAtThresholdAndPersistsRotation verifies fresh request authorization.
func TestDriverStreamRefreshesAtThresholdAndPersistsRotation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	accountID := "account-refresh"
	oldAccess := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		},
	)
	newAccess := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
			"rotated":                     true,
		},
	)
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().
		Load().
		Return(testCredentialPayload(t, oldAccess, "refresh-old", accountID, now.Add(5*time.Minute)), true, nil)
	credentials.EXPECT().Save(gomock.Any()).DoAndReturn(func(payload []byte) error {
		var rotated oauthCredentials
		require.NoError(t, json.Unmarshal(payload, &rotated))
		assert.Equal(t, newAccess, rotated.AccessToken)
		assert.Equal(t, "refresh-new", rotated.RefreshToken)
		assert.Equal(t, accountID, rotated.AccountID)
		return nil
	})
	interaction := NewMockInteraction(gomock.NewController(t))
	var tokenRequests atomic.Int32
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/token":
				tokenRequests.Add(1)
				var body map[string]string
				assert.NoError(t, json.UnmarshalRead(request.Body, &body))
				assert.Equal(t, "refresh_token", body["grant_type"])
				assert.Equal(t, "refresh-old", body["refresh_token"])
				writer.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(
					writer,
					`{"access_token":%q,"refresh_token":"refresh-new","expires_in":3600}`,
					newAccess,
				)
			case "/responses":
				assert.Equal(t, "Bearer "+newAccess, request.Header.Get("Authorization"))
				assert.Equal(t, accountID, request.Header.Get("chatgpt-account-id"))
				writeSSE(
					writer,
					completedEvent(
						`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`,
					),
				)
			default:
				http.NotFound(writer, request)
			}
		}),
	)
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	options.tokenURL = server.URL + "/token"
	options.now = func() time.Time { return now }
	service := newDriver(testConfig(), credentials, interaction, options)

	events, err := collectStreamEvents(
		service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model:           testModelDescriptor("gpt-test"),
			History: []agent.HistoryEntry{
				{
					Model:      mo.None[model.Response](),
					ToolResult: mo.None[agent.ToolResult](),
					Kind:       agent.HistoryEntryUser,
					User:       mo.Some(model.TextMessage("hello")),
				},
			},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)
	response := terminalResponse(events)

	require.NoError(t, err)
	assert.Equal(t, model.OutcomeStop, response.Outcome.OrEmpty())
	assert.Equal(t, int32(1), tokenRequests.Load())
}

// TestDriverStreamSkipsRefreshOutsideThreshold verifies six remaining minutes use loaded credentials.
func TestDriverStreamSkipsRefreshOutsideThreshold(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	accountID := "account-fresh"
	accessToken := testJWT(
		t,
		map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		},
	)
	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().
		Load().
		Return(testCredentialPayload(t, accessToken, "refresh", accountID, now.Add(6*time.Minute)), true, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			assert.Equal(t, "Bearer "+accessToken, request.Header.Get("Authorization"))
			writeSSE(
				writer,
				completedEvent(
					`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"done","annotations":[],"logprobs":[]}]}]`,
				),
			)
		}),
	)
	t.Cleanup(server.Close)
	options := testProviderOptions(server)
	options.now = func() time.Time { return now }
	service := newDriver(testConfig(), credentials, interaction, options)

	_, err := collectStreamEvents(
		service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model:           testModelDescriptor("gpt-test"),
			History: []agent.HistoryEntry{
				{
					Model:      mo.None[model.Response](),
					ToolResult: mo.None[agent.ToolResult](),
					Kind:       agent.HistoryEntryUser,
					User:       mo.Some(model.TextMessage("hello")),
				},
			},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)

	require.NoError(t, err)
}

// TestDriverStreamMissingCredentialsDoesNotStartOAuth verifies headless failure requires no interaction.
func TestDriverStreamMissingCredentialsDoesNotStartOAuth(t *testing.T) {
	t.Parallel()

	credentials := NewMockCredentials(gomock.NewController(t))
	credentials.EXPECT().Load().Return(nil, false, nil)
	interaction := NewMockInteraction(gomock.NewController(t))
	service := New(testConfig(), credentials, interaction)

	events, err := collectStreamEvents(service,
		t.Context(),
		run.ModelRequest{
			ReasoningChoice: model.ReasoningChoiceOn,
			Instructions:    "instructions",
			Model:           testModelDescriptor("gpt-test"),
			History: []agent.HistoryEntry{{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("hello")),
			}},
			Tools: nil,
		},
		func(run.StreamEvent) error { return nil },
	)
	response := terminalResponse(events)

	require.ErrorIs(t, err, ErrSignInRequired)
	assert.Equal(t, model.OutcomeFailed, response.Outcome.OrEmpty())
	assert.Equal(t, signInRequiredMessage, response.ErrorMessage.OrEmpty())
}

// TestDriverStreamLoadsCredentialsForEveryRequest verifies access data is never cached across model calls.
func TestDriverStreamLoadsCredentialsForEveryRequest(t *testing.T) {
	t.Parallel()

	accountID := "account-fresh-load"
	firstAccess := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		"request":                     1,
	})
	secondAccess := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
		"request":                     2,
	})
	credentials := NewMockCredentials(gomock.NewController(t))
	gomock.InOrder(
		credentials.EXPECT().Load().Return(
			testCredentialPayload(
				t,
				firstAccess,
				"refresh",
				accountID,
				time.Now().Add(time.Hour),
			), true, nil,
		),
		credentials.EXPECT().Load().Return(
			testCredentialPayload(
				t,
				secondAccess,
				"refresh",
				accountID,
				time.Now().Add(time.Hour),
			), true, nil,
		),
	)
	interaction := NewMockInteraction(gomock.NewController(t))
	authorizations := make(chan string, 2)
	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			authorizations <- request.Header.Get("Authorization")
			writeSSE(writer, completedEvent(
				`[{"id":"m","type":"message","role":"assistant","status":"completed","content":[]}]`,
			))
		}),
	)
	t.Cleanup(server.Close)
	service := newDriver(
		testConfig(), credentials, interaction, testProviderOptions(server),
	)
	request := run.ModelRequest{
		ReasoningChoice: model.ReasoningChoiceOn,
		Instructions:    "instructions",
		Model:           testModelDescriptor("model"),
		History: []agent.HistoryEntry{
			{
				Model:      mo.None[model.Response](),
				ToolResult: mo.None[agent.ToolResult](),
				Kind:       agent.HistoryEntryUser,
				User:       mo.Some(model.TextMessage("hello")),
			},
		},
		Tools: nil,
	}

	_, firstErr := collectStreamEvents(
		service,
		t.Context(),
		request,
		func(run.StreamEvent) error { return nil },
	)
	_, secondErr := collectStreamEvents(
		service,
		t.Context(),
		request,
		func(run.StreamEvent) error { return nil },
	)

	require.NoError(t, firstErr)
	require.NoError(t, secondErr)
	assert.Equal(t, "Bearer "+firstAccess, <-authorizations)
	assert.Equal(t, "Bearer "+secondAccess, <-authorizations)
}
