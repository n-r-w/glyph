package app

import (
	"bytes"

	"encoding/json"
	"errors"
	"fmt"

	"net/http"

	"os"

	"path/filepath"

	"testing"

	"google.golang.org/grpc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"

	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

type sessionUsageRestartObservation struct {
	BeforeInfo       sessionInfoObservation `json:"before_info"`
	AfterInfo        sessionInfoObservation `json:"after_info"`
	NewStartup       sessionInfoObservation `json:"new_startup"`
	BeforeStatistics statisticsObservation  `json:"before_statistics"`
	AfterStatistics  statisticsObservation  `json:"after_statistics"`
	Complete         bool                   `json:"complete"`
}

func runSessionUsageRestartUI(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	initialization *uipb.Initialization,
) error {
	tracePath := os.Getenv(appUITraceEnvironment)
	payload, readErr := os.ReadFile(tracePath)
	if errors.Is(readErr, os.ErrNotExist) {
		return recordInitialSessionUsage(stream, tracePath)
	}
	if readErr != nil {
		return readErr
	}
	var observation sessionUsageRestartObservation
	if err := json.Unmarshal(payload, &observation); err != nil {
		return err
	}
	observation.NewStartup = observeSessionInfo(initialization.GetSessionInfo())
	if observation.NewStartup.ID == observation.BeforeInfo.ID || observation.NewStartup.StoragePathPresent {
		return errors.New("usage restart did not create an empty active session")
	}
	if err := configureRestartSelection(stream); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active ResumeSession field.
	if err := stream.Send(uipb.OpenResponse_builder{ResumeSession: uipb.ResumeSessionCommand_builder{
		SessionId: new(observation.BeforeInfo.ID),
	}.Build()}.Build()); err != nil {
		return err
	}
	changed, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionChanged() != nil
	})
	if err != nil {
		return err
	}
	if observeSessionInfo(changed.GetSessionChanged().GetInfo()) != observation.BeforeInfo {
		return errors.New("usage restart changed session information during resume")
	}
	frame, err := requestUISessionInformation(stream)
	if err != nil {
		return err
	}
	observation.AfterInfo = observeSessionInfo(frame.GetInfo())
	observation.AfterStatistics = observeUIStatistics(frame.GetStatistics())
	observation.Complete = true
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err = os.WriteFile(tracePath, encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

// recordInitialSessionUsage stores the first-process session snapshot before requesting shutdown.
func recordInitialSessionUsage(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
	tracePath string,
) error {
	if err := configureRestartSelection(stream); err != nil {
		return err
	}
	if os.Getenv(appUICostStateEnvironment) == "empty" {
		if err := persistEmptyUsageSession(stream); err != nil {
			return err
		}
	} else if err := submitRestartTurn(stream, "usage request"); err != nil {
		return err
	}
	frame, err := requestUISessionInformation(stream)
	if err != nil {
		return err
	}
	observation := sessionUsageRestartObservation{
		BeforeInfo: observeSessionInfo(frame.GetInfo()), AfterInfo: sessionInfoObservation{},
		NewStartup: sessionInfoObservation{}, BeforeStatistics: observeUIStatistics(frame.GetStatistics()),
		AfterStatistics: statisticsObservation{}, Complete: false,
	}
	encoded, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	if err = os.WriteFile(tracePath, encoded, 0o600); err != nil {
		return err
	}
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active Quit field.
	return stream.Send(uipb.OpenResponse_builder{Quit: &uipb.QuitCommand{}}.Build())
}

// persistEmptyUsageSession names the empty session so the next Host process can resume it.
func persistEmptyUsageSession(stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse]) error {
	name := "empty cost session"
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active SetSessionName field.
	if err := stream.Send(uipb.OpenResponse_builder{
		SetSessionName: uipb.SetSessionNameCommand_builder{Name: &name}.Build(),
	}.Build()); err != nil {
		return err
	}
	_, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionInformation() != nil
	})
	return err
}

func requestUISessionInformation(
	stream grpc.BidiStreamingServer[uipb.OpenRequest, uipb.OpenResponse],
) (*uipb.SessionInformation, error) {
	//nolint:exhaustruct_v5 // uipb.OpenResponse_builder sets only the active GetSessionInfo field.
	if err := stream.Send(uipb.OpenResponse_builder{GetSessionInfo: &uipb.GetSessionInfoCommand{}}.Build()); err != nil {
		return nil, err
	}
	frame, err := receiveSessionFrame(stream, func(frame *uipb.OpenRequest) bool {
		return frame.GetSessionInformation() != nil
	})
	if err != nil {
		return nil, err
	}
	return frame.GetSessionInformation(), nil
}

func observeUIStatistics(statistics *uipb.SessionStatistics) statisticsObservation {
	observation := statisticsObservation{
		UserMessages: statistics.GetUserMessages(), ModelResponses: statistics.GetModelResponses(),
		ToolCalls: statistics.GetToolCalls(), ToolResults: statistics.GetToolResults(),
		TotalMessages: statistics.GetTotalMessages(), Tokens: tokenUsageObservation{},
		EstimatedCost:  observeUICost(statistics.GetEstimatedCost()),
		CostGroupCount: len(statistics.GetCostBreakdown()), GroupProvider: "", GroupModel: "",
		GroupCost: costObservation{},
	}
	if len(statistics.GetCostBreakdown()) == 1 {
		group := statistics.GetCostBreakdown()[0]
		observation.GroupProvider = group.GetProviderId()
		observation.GroupModel = group.GetModelId()
		observation.GroupCost = observeUICost(group.GetEstimatedCost())
	}
	if tokens := statistics.GetTokens(); tokens != nil {
		observation.Tokens = tokenUsageObservation{
			Present: true, InputTokens: tokens.GetInputTokens(), OutputTokens: tokens.GetOutputTokens(),
			CacheReadTokens: tokens.GetCacheReadTokens(), CacheWriteTokens: tokens.GetCacheWriteTokens(),
			ReasoningTokens: tokens.GetReasoningTokens(), TotalTokens: tokens.GetTotalTokens(),
		}
	}
	return observation
}

// observeUICost preserves optional UI cost fields for process-boundary assertions.
func observeUICost(cost *uipb.EstimatedCost) costObservation {
	if cost == nil {
		return costObservation{}
	}
	return costObservation{
		Present: true, Input: cost.GetInput(), Output: cost.GetOutput(),
		CacheRead: cost.GetCacheRead(), CacheWrite: cost.GetCacheWrite(), Total: cost.GetTotal(),
	}
}

// assertStatisticsObservation compares every count, token, cost, and provider-model field.
func assertStatisticsObservation(t *testing.T, expected, actual statisticsObservation) {
	t.Helper()

	assert.Equal(t, expected.UserMessages, actual.UserMessages)
	assert.Equal(t, expected.ModelResponses, actual.ModelResponses)
	assert.Equal(t, expected.ToolCalls, actual.ToolCalls)
	assert.Equal(t, expected.ToolResults, actual.ToolResults)
	assert.Equal(t, expected.TotalMessages, actual.TotalMessages)
	assert.Equal(t, expected.Tokens, actual.Tokens)
	assertCostObservation(t, expected.EstimatedCost, actual.EstimatedCost)
	assert.Equal(t, expected.CostGroupCount, actual.CostGroupCount)
	assert.Equal(t, expected.GroupProvider, actual.GroupProvider)
	assert.Equal(t, expected.GroupModel, actual.GroupModel)
	assertCostObservation(t, expected.GroupCost, actual.GroupCost)
}

// assertCostObservation compares presence and all five cost fields with floating tolerance.
func assertCostObservation(t *testing.T, expected, actual costObservation) {
	t.Helper()

	assert.Equal(t, expected.Present, actual.Present)
	assert.InDelta(t, expected.Input, actual.Input, 1e-12)
	assert.InDelta(t, expected.Output, actual.Output, 1e-12)
	assert.InDelta(t, expected.CacheRead, actual.CacheRead, 1e-12)
	assert.InDelta(t, expected.CacheWrite, actual.CacheWrite, 1e-12)
	assert.InDelta(t, expected.Total, actual.Total, 1e-12)
}

// TestRunWithPathsUISessionUsageSurvivesRestart verifies all cost states through Host reconstruction.
func TestRunWithPathsUISessionUsageSurvivesRestart(t *testing.T) {
	// Arrange the complete cost-state matrix for the existing UI helper process.
	tests := []struct {
		name        string
		state       string
		usage       string
		namePresent bool
		expected    statisticsObservation
	}{
		{
			name: "empty session", state: "empty", usage: "", namePresent: true,
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
			name: "known zero", state: "known-zero", namePresent: false,
			usage: `{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}`,
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{
					Present: true, InputTokens: 0, OutputTokens: 0, CacheReadTokens: 0,
					CacheWriteTokens: 0, ReasoningTokens: 0, TotalTokens: 0,
				},
				EstimatedCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
				CostGroupCount: 1, GroupProvider: "openai-codex", GroupModel: "selected-model",
				GroupCost: costObservation{
					Present: true, Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
				},
			},
		},
		{
			name: "available nonzero", state: "nonzero", namePresent: false,
			usage: `{"input_tokens":10,"output_tokens":4,"total_tokens":99,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1},"output_tokens_details":{"reasoning_tokens":3}}`,
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
				CostGroupCount: 1, GroupProvider: "openai-codex", GroupModel: "selected-model",
				GroupCost: costObservation{
					Present: true, Input: 0.000007, Output: 0.000008,
					CacheRead: 0.000006, CacheWrite: 0.000004, Total: 0.000025,
				},
			},
		},
		{
			name: "unavailable", state: "unavailable", usage: "", namePresent: false,
			expected: statisticsObservation{
				UserMessages: 1, ModelResponses: 1, ToolCalls: 0, ToolResults: 0, TotalMessages: 2,
				Tokens: tokenUsageObservation{}, EstimatedCost: costObservation{}, CostGroupCount: 1,
				GroupProvider: "openai-codex", GroupModel: "selected-model", GroupCost: costObservation{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Arrange persistent Host paths, the real UI helper process, and one matrix state.
			paths := testPaths(t, pricedRestartSelectionSettings())
			accessToken := semanticAccessToken(t, "account")
			require.NoError(t, os.WriteFile(paths.CredentialsFile, fmt.Appendf(
				nil,
				`{"version":1,"providers":{"openai-codex":{"access_token":%q,"refresh_token":"refresh","account_id":"account","expires_at":"2099-01-01T00:00:00Z"}}}`,
				accessToken,
			), 0o600))
			previousTransport := http.DefaultTransport
			http.DefaultTransport = usageCodexTransport{usageJSON: test.usage}
			t.Cleanup(func() { http.DefaultTransport = previousTransport })
			uiDirectory := t.TempDir()
			writeUIExecutable(t, uiDirectory, "Session_Usage_Restart_UI")
			tracePath := filepath.Join(t.TempDir(), "session-usage-restart.json")
			t.Setenv(appUITraceEnvironment, tracePath)
			t.Setenv(appUIBehaviorEnvironment, "session-usage-restart")
			t.Setenv(appUICostStateEnvironment, test.state)
			command := cli.Command{
				Mode: cli.ModeUI, Headless: headless.Command{UserText: "", ExtensionDirectory: t.TempDir()},
				ExtensionDirectory: t.TempDir(), UIDirectory: uiDirectory,
				UIID: "session-usage-restart-ui", SocketPath: "",
			}

			// Act by running the Host, restarting it, and explicitly resuming the stored session.
			require.NoError(t, runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{}))
			require.NoError(t, runWithPaths(t.Context(), paths, command, &bytes.Buffer{}, &bytes.Buffer{}))
			payload, err := os.ReadFile(tracePath)
			require.NoError(t, err)
			var observation sessionUsageRestartObservation
			require.NoError(t, json.Unmarshal(payload, &observation))

			// Assert all session fields, counts, token presence, buckets, and nondefault selection survive.
			require.True(t, observation.Complete)
			assert.Equal(t, observation.BeforeInfo, observation.AfterInfo)
			assert.NotEqual(t, observation.BeforeInfo.ID, observation.NewStartup.ID)
			assert.True(t, observation.BeforeInfo.IDPresent)
			assert.True(t, observation.BeforeInfo.WorkingDirectoryPresent)
			assert.True(t, observation.BeforeInfo.StoragePathPresent)
			assert.True(t, observation.BeforeInfo.CreatedTimePresent)
			assert.True(t, observation.BeforeInfo.UpdateTimePresent)
			assert.Equal(t, test.namePresent, observation.BeforeInfo.NamePresent)
			assertStatisticsObservation(t, test.expected, observation.BeforeStatistics)
			assertStatisticsObservation(t, test.expected, observation.AfterStatistics)
		})
	}
}
