package sessiontree

import (
	"github.com/samber/mo"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// SummaryMode identifies branch-summary behavior for one navigation.
type SummaryMode uint8

const (
	// SummaryModeNoSummary disables branch summarization.
	SummaryModeNoSummary SummaryMode = iota
	// SummaryModeSummarize requests built-in branch summarization.
	SummaryModeSummarize
	// SummaryModeSummarizeWithCustomPrompt adds a caller-supplied focus to built-in summarization.
	SummaryModeSummarizeWithCustomPrompt
)

// NavigationRequest contains the navigation intent composed by request handlers.
type NavigationRequest struct {
	// TargetEntryID identifies the selected tree entry.
	TargetEntryID string
	// SummaryMode identifies requested branch-summary behavior.
	SummaryMode SummaryMode
	// CustomFocus contains the required focus only for custom-prompt mode.
	CustomFocus mo.Option[string]
}

var (
	// ErrModelUnavailable reports a missing configured model or unsupported reasoning choice.
	ErrModelUnavailable = &navigationError{
		code: controllerui.FailureCodeModelUnavailable,
		text: "summary model unavailable",
	}
	// ErrCredentialUnavailable reports unavailable credentials for the configured summary model.
	ErrCredentialUnavailable = &navigationError{
		code: controllerui.FailureCodeProviderAuth,
		text: "summary model credential unavailable",
	}
	// ErrModelFailed reports a failed or invalid summary-model response.
	ErrModelFailed = &navigationError{code: controllerui.FailureCodeModelFailed, text: "summary model failed"}
	// ErrExtensionInvalidResult reports invalid final handler-produced state.
	ErrExtensionInvalidResult = &navigationError{
		code: controllerui.FailureCodeExtensionInvalid,
		text: "extension produced invalid session tree state",
	}
	// ErrExtensionUnavailable reports a failed extension process or protocol call.
	ErrExtensionUnavailable = &navigationError{
		code: controllerui.FailureCodeExtension,
		text: "session tree extension unavailable",
	}
)
