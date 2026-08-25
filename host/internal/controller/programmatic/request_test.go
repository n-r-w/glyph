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

			request: programmaticv1.OpenRequest_builder{
				CorrelationId:         proto.String("missing"),
				UserRequest:           nil,
				Abort:                 nil,
				GetRunState:           nil,
				GetMessages:           nil,
				GetModels:             nil,
				SelectModel:           nil,
				SelectReasoningChoice: nil,
			}.Build(),
			want: Command{
				CorrelationID:   "missing",
				Kind:            CommandUnspecified,
				UserText:        "",
				ProviderID:      "",
				ModelID:         "",
				ReasoningChoice: "",
			},
		},
		"user request": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("user"),
				UserRequest: programmaticv1.UserRequest_builder{
					Text: proto.String("  exact text  "),
				}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "user",
				Kind:            CommandUserRequest,
				UserText:        "  exact text  ",
				ProviderID:      "",
				ModelID:         "",
				ReasoningChoice: "",
			},
		},
		"invalid user request": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("invalid"),
				UserRequest: programmaticv1.UserRequest_builder{
					Text: proto.String(" \t\n"),
				}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "invalid",
				Kind:            CommandUserRequest,
				UserText:        " \t\n",
				ProviderID:      "",
				ModelID:         "",
				ReasoningChoice: "",
			},
		},
		"abort": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active Abort field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("abort"),
				Abort:         programmaticv1.Abort_builder{}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "abort",
				Kind:            CommandAbort,
				UserText:        "",
				ProviderID:      "",
				ModelID:         "",
				ReasoningChoice: "",
			},
		},
		"get run state": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("state"),
				GetRunState:   programmaticv1.GetRunState_builder{}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "state",
				Kind:            CommandGetRunState,
				UserText:        "",
				ProviderID:      "",
				ModelID:         "",
				ReasoningChoice: "",
			},
		},
		"get messages": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active GetMessages field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("messages"),
				GetMessages:   programmaticv1.GetMessages_builder{}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "messages",
				Kind:            CommandGetMessages,
				UserText:        "",
				ProviderID:      "",
				ModelID:         "",
				ReasoningChoice: "",
			},
		},
		"get models": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active GetModels field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("models"),
				GetModels:     programmaticv1.GetModels_builder{}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "models",
				Kind:            CommandGetModels,
				UserText:        "",
				ProviderID:      "",
				ModelID:         "",
				ReasoningChoice: "",
			},
		},
		"select model": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active SelectModel field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("select-model"),
				SelectModel: programmaticv1.SelectModel_builder{
					ProviderId: proto.String("provider"),
					ModelId:    proto.String("model"),
				}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "select-model",
				Kind:            CommandSelectModel,
				ProviderID:      "provider",
				ModelID:         "model",
				UserText:        "",
				ReasoningChoice: "",
			},
		},
		"select reasoning": {
			//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active SelectReasoningChoice field.
			request: programmaticv1.OpenRequest_builder{
				CorrelationId: proto.String("select-reasoning"),
				SelectReasoningChoice: programmaticv1.SelectReasoningChoice_builder{
					Choice: programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX.Enum(),
				}.Build(),
			}.Build(),
			want: Command{
				CorrelationID:   "select-reasoning",
				Kind:            CommandSelectReasoningChoice,
				ReasoningChoice: "max",
				UserText:        "",
				ProviderID:      "",
				ModelID:         "",
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
		//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active SelectReasoningChoice field.
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

	//nolint:exhaustruct // programmaticv1.OpenRequest_builder sets only the active Abort field.
	request := programmaticv1.OpenRequest_builder{
		Abort: programmaticv1.Abort_builder{}.Build(),
	}.Build()
	_, err := mapOpenRequest(request)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = mapOpenRequest(nil)
	require.Error(t, err)
	_, isStatus := status.FromError(err)
	assert.False(t, isStatus)
}
