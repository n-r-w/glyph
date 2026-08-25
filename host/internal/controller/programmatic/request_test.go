//nolint:exhaustruct // Protobuf oneof builders set only the active command field.
package programmatic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapOpenRequestPreservesEveryCommand verifies the complete request oneof mapping.
func TestMapOpenRequestPreservesEveryCommand(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		request *programmaticv1.OpenRequest
		want    Command
	}{
		"missing command": {
			request: programmaticv1.OpenRequest_builder{CorrelationId: proto.String("missing")}.Build(),
			want:    Command{CorrelationID: "missing", Kind: CommandUnspecified, UserText: ""},
		},
		"user request": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("user"),
				UserRequest:   programmaticv1.UserRequest_builder{Text: proto.String("  exact text  ")}.Build(),
			}.Build(),
			want: Command{CorrelationID: "user", Kind: CommandUserRequest, UserText: "  exact text  "},
		},
		"invalid user request": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("invalid"),
				UserRequest:   programmaticv1.UserRequest_builder{Text: proto.String(" \t\n")}.Build(),
			}.Build(),
			want: Command{CorrelationID: "invalid", Kind: CommandUserRequest, UserText: " \t\n"},
		},
		"abort": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("abort"), Abort: programmaticv1.Abort_builder{}.Build(),
			}.Build(),
			want: Command{CorrelationID: "abort", Kind: CommandAbort, UserText: ""},
		},
		"get run state": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("state"), GetRunState: programmaticv1.GetRunState_builder{}.Build(),
			}.Build(),
			want: Command{CorrelationID: "state", Kind: CommandGetRunState, UserText: ""},
		},
		"get messages": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("messages"), GetMessages: programmaticv1.GetMessages_builder{}.Build(),
			}.Build(),
			want: Command{CorrelationID: "messages", Kind: CommandGetMessages, UserText: ""},
		},
		"get models": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("models"), GetModels: programmaticv1.GetModels_builder{}.Build(),
			}.Build(),
			want: Command{CorrelationID: "models", Kind: CommandGetModels},
		},
		"select model": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("select-model"),
				SelectModel: programmaticv1.SelectModel_builder{
					ProviderId: proto.String("provider"), ModelId: proto.String("model"),
				}.Build(),
			}.Build(),
			want: Command{
				CorrelationID: "select-model", Kind: CommandSelectModel,
				ProviderID: "provider", ModelID: "model",
			},
		},
		"select reasoning": {
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("select-reasoning"),
				SelectReasoningChoice: programmaticv1.SelectReasoningChoice_builder{
					Choice: programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX.Enum(),
				}.Build(),
			}.Build(),
			want: Command{
				CorrelationID: "select-reasoning", Kind: CommandSelectReasoningChoice, ReasoningChoice: "max",
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := mapOpenRequest(test.request)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

// TestMapOpenRequestMapsReasoningChoices verifies every transport reasoning value.
func TestMapOpenRequestMapsReasoningChoices(t *testing.T) {
	t.Parallel()

	tests := map[programmaticv1.ReasoningChoice]string{
		programmaticv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED: "",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF:         "off",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_ON:          "on",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:     "minimal",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW:         "low",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:      "medium",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH:        "high",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_XHIGH:       "xhigh",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX:         "max",
		programmaticv1.ReasoningChoice(99):                          "",
	}
	for level, want := range tests {
		request := programmaticv1.OpenRequest_builder{
			CorrelationId: proto.String(level.String()),
			SelectReasoningChoice: programmaticv1.SelectReasoningChoice_builder{
				Choice: level.Enum(),
			}.Build(),
		}.Build()
		got, err := mapOpenRequest(request)
		require.NoError(t, err)
		assert.Equal(t, want, string(got.ReasoningChoice))
	}
}

// TestMapOpenRequestRejectsTerminalFrames verifies uncorrelated and malformed frame handling.
func TestMapOpenRequestRejectsTerminalFrames(t *testing.T) {
	t.Parallel()

	request := programmaticv1.OpenRequest_builder{Abort: programmaticv1.Abort_builder{}.Build()}.Build()
	_, err := mapOpenRequest(request)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = mapOpenRequest(nil)
	require.Error(t, err)
	_, isStatus := status.FromError(err)
	assert.False(t, isStatus)
}
