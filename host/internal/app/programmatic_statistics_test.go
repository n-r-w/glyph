//go:build integration

package app

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestSessionUsageAvailabilitySurvivesRestart verifies all cost states through the real RPC process.
func (testSuite *ProgrammaticAppSuite) TestSessionUsageAvailabilitySurvivesRestart() {
	t := testSuite.T()
	// Arrange the complete cost-state matrix for the existing Programmatic process fixture.
	tests := []struct {
		name      string
		submit    bool
		usageJSON string
		expected  statisticsObservation
	}{
		{
			name: "empty session", submit: false, usageJSON: "",
			expected: statisticsObservation{
				UserMessages: 0, ModelResponses: 0, ToolCalls: 0, ToolResults: 0, TotalMessages: 0,
				Tokens: tokenUsageObservation{
					Present: true, InputTokens: 0, OutputTokens: 0, CacheReadTokens: 0,
					CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
				},
				EstimatedCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
				CostGroupCount: 0, GroupProvider: "", GroupModel: "", GroupCost: costObservation{},
			},
		},
		{
			name:   "known zero",
			submit: true,
			usageJSON: `{"input_tokens":0,"output_tokens":0,"total_tokens":0,` +
				`"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}` +
				`,"output_tokens_details":{"reasoning_tokens":0}}`,
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{
					Present: true, InputTokens: 0, OutputTokens: 0, CacheReadTokens: 0,
					CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
				},
				EstimatedCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
				CostGroupCount: 1, GroupProvider: "openai-codex", GroupModel: "gpt-test",
				GroupCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
			},
		},
		{
			name:   "available nonzero",
			submit: true,
			usageJSON: `{"input_tokens":10,"output_tokens":4,"total_tokens":99,` +
				`"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1}` +
				`,"output_tokens_details":{"reasoning_tokens":3}}`,
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{
					Present: true, InputTokens: 7, OutputTokens: 4, CacheReadTokens: 2,
					CacheWriteTokens: 1, ReasoningTokens: 3, TotalTokens: 14,
				},
				EstimatedCost: costObservation{
					Present: true, Input: 0.000007, Output: 0.000008,
					CacheRead: 0.000006, CacheWrite: 0.000004, Total: 0.000025,
				},
				CostGroupCount: 1, GroupProvider: "openai-codex", GroupModel: "gpt-test",
				GroupCost: costObservation{
					Present: true, Input: 0.000007, Output: 0.000008,
					CacheRead: 0.000006, CacheWrite: 0.000004, Total: 0.000025,
				},
			},
		},
		{
			name: "unavailable", submit: true, usageJSON: "",
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{}, EstimatedCost: costObservation{}, CostGroupCount: 1,
				GroupProvider: "openai-codex", GroupModel: "gpt-test", GroupCost: costObservation{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange one durable matrix state and persistent process paths.
			paths := testPaths(t, pricedCodexSettings())
			accessToken := semanticAccessToken(t, "account")
			credentials := fmt.Sprintf(
				`{"version":1,"providers":{"openai-codex":{"access_token":%q,`+
					`"refresh_token":"refresh","account_id":"account",`+
					`"expires_at":"2099-01-01T00:00:00Z"}}}`,
				accessToken,
			)
			require.NoError(t, os.WriteFile(paths.CredentialsFile, []byte(credentials), 0o600))
			previousTransport := http.DefaultTransport
			http.DefaultTransport = usageCodexTransport{usageJSON: test.usageJSON}
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			fixture := startProgrammaticFixture(t, paths)

			// Act by persisting the state and querying statistics before and after explicit resume.
			if test.submit {
				require.NoError(t, fixture.stream.Send(userRequest("usage", "usage request")))
				_, err := fixture.stream.Recv()
				require.NoError(t, err)
				waitProgrammaticSettled(t, fixture)
			} else {
				request := new(programmaticv1.OpenRequest)
				request.SetCorrelationId("persist-empty")
				request.SetSetSessionName(
					programmaticv1.SetSessionName_builder{Name: new("empty cost session")}.Build(),
				)
				require.NoError(t, fixture.stream.Send(request))
				_, err := fixture.stream.Recv()
				require.NoError(t, err)
			}
			before := requestProgrammaticStatistics(t, fixture.stream, "before")
			info := requestProgrammaticInfo(t, fixture.stream, "info")
			fixture.closeOwner(t)
			restarted := startProgrammaticFixture(t, paths)
			defer restarted.closeOwner(t)
			request := new(programmaticv1.OpenRequest)
			request.SetCorrelationId("resume")
			request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new(info.GetId())}.Build())
			require.NoError(t, restarted.stream.Send(request))
			resumeResponse, err := restarted.stream.Recv()
			require.NoError(t, err)
			after := requestProgrammaticStatistics(t, restarted.stream, "after")

			// Assert identity and every accounting field survive JSONL reopen and Host reconstruction.
			assertProgrammaticSessionInfoEqual(t, info, resumeResponse.GetCommandResponse().GetSessionInfo().GetInfo())
			assertProgrammaticStatisticsObservation(t, test.expected, before)
			assertProgrammaticStatisticsObservation(t, test.expected, after)
		})
	}
}

// assertProgrammaticStatisticsObservation compares all Programmatic accounting fields.
func assertProgrammaticStatisticsObservation(
	t *testing.T,
	expected statisticsObservation,
	statistics *programmaticv1.SessionStatistics,
) {
	t.Helper()
	assertStatisticsObservation(t, expected, observeProgrammaticStatistics(statistics))
}

// observeProgrammaticStatistics preserves all Programmatic statistics fields for matrix assertions.
func observeProgrammaticStatistics(statistics *programmaticv1.SessionStatistics) statisticsObservation {
	observation := statisticsObservation{
		UserMessages: statistics.GetUserMessages(), ModelResponses: statistics.GetModelResponses(),
		ToolCalls: statistics.GetToolCalls(), ToolResults: statistics.GetToolResults(),
		TotalMessages: statistics.GetTotalMessages(), Tokens: tokenUsageObservation{},
		EstimatedCost:  observeProgrammaticCost(statistics.GetEstimatedCost()),
		CostGroupCount: len(statistics.GetCostBreakdown()), GroupProvider: "", GroupModel: "",
		GroupCost: costObservation{},
	}
	if tokens := statistics.GetTokens(); tokens != nil {
		observation.Tokens = tokenUsageObservation{
			Present: true, InputTokens: tokens.GetInputTokens(), OutputTokens: tokens.GetOutputTokens(),
			CacheReadTokens: tokens.GetCacheReadTokens(), CacheWriteTokens: tokens.GetCacheWriteTokens(),
			ReasoningTokens: tokens.GetReasoningTokens(), TotalTokens: tokens.GetTotalTokens(),
		}
	}
	if len(statistics.GetCostBreakdown()) == 1 {
		group := statistics.GetCostBreakdown()[0]
		observation.GroupProvider = group.GetProviderId()
		observation.GroupModel = group.GetModelId()
		observation.GroupCost = observeProgrammaticCost(group.GetEstimatedCost())
	}
	return observation
}

// observeProgrammaticCost preserves optional Programmatic cost fields for process assertions.
func observeProgrammaticCost(cost *programmaticv1.EstimatedCost) costObservation {
	if cost == nil {
		return costObservation{}
	}
	return costObservation{
		Present: true, Input: cost.GetInput(), Output: cost.GetOutput(),
		CacheRead: cost.GetCacheRead(), CacheWrite: cost.GetCacheWrite(), Total: cost.GetTotal(),
	}
}

func requestProgrammaticStatistics(
	t *testing.T,
	stream grpc.BidiStreamingClient[programmaticv1.OpenRequest, programmaticv1.OpenResponse],
	correlationID string,
) *programmaticv1.SessionStatistics {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetCorrelationId(correlationID)
	request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
	require.NoError(t, stream.Send(request))
	response, err := stream.Recv()
	require.NoError(t, err)
	return response.GetCommandResponse().GetSessionStats().GetStatistics()
}

func requestProgrammaticInfo(
	t *testing.T,
	stream grpc.BidiStreamingClient[programmaticv1.OpenRequest, programmaticv1.OpenResponse],
	correlationID string,
) *programmaticv1.SessionInfo {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetCorrelationId(correlationID)
	request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
	require.NoError(t, stream.Send(request))
	response, err := stream.Recv()
	require.NoError(t, err)
	return response.GetCommandResponse().GetSessionInfo().GetInfo()
}

type usageCodexTransport struct {
	usageJSON string
}

func (transport usageCodexTransport) RoundTrip(*http.Request) (*http.Response, error) {
	usageField := ""
	if transport.usageJSON != "" {
		usageField = fmt.Sprintf(",\"usage\":%s", transport.usageJSON)
	}
	body := fmt.Sprintf(
		"data: {\"type\":\"response.completed\","+
			"\"response\":{\"id\":\"usage-response\","+
			"\"model\":\"selected-model\",\"status\":\"completed\","+
			"\"service_tier\":\"default\",\"metadata\":{}%s,\"output\":[]}}\n\n"+
			"data: [DONE]\n\n",
		usageField,
	)
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header),
		Status: "", Proto: "", ProtoMajor: 0, ProtoMinor: 0, ContentLength: 0, TransferEncoding: nil,
		Close: false, Uncompressed: false, Trailer: nil, Request: nil, TLS: nil,
	}, nil
}
