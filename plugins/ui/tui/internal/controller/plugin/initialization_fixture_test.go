package plugin

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// validInitialization creates the smallest complete TUI initialization payload.
func validInitialization() *uiv1.Initialization {
	selection := uiv1.ModelSelection_builder{
		ProviderId: new("provider"), ModelId: new("model"),
		ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_OFF),
	}.Build()
	reasoning := uiv1.ReasoningCapabilities_builder{
		Supported: new(false), Choices: nil, DefaultChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_OFF),
	}.Build()
	model := uiv1.ConfiguredModel_builder{
		ProviderId: new("provider"), ModelId: new("model"), Reasoning: reasoning,
	}.Build()
	now := timestamppb.New(time.Unix(1, 0))
	session := uiv1.SessionInfo_builder{
		Id: new("session"), Name: nil, WorkingDirectory: new("/project"), StoragePath: nil,
		CreatedTime: now, UpdateTime: now,
	}.Build()
	return uiv1.Initialization_builder{
		SelectedUiId: new("glyph-tui"), StartupContent: nil, Extensions: nil,
		Availability: new(uiv1.Availability_AVAILABILITY_IDLE), Models: []*uiv1.ConfiguredModel{model},
		ModelSelection: selection, SessionInfo: session,
	}.Build()
}
