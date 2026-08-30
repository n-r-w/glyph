package sessionnavigation

import (
	"errors"

	"github.com/samber/mo"
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

// Request contains one validated client-neutral tree-navigation request.
type Request struct {
	// TargetEntryID identifies the selected tree entry.
	TargetEntryID string
	// SummaryMode identifies requested branch-summary behavior.
	SummaryMode SummaryMode
	// CustomFocus contains the required focus only for custom-prompt mode.
	CustomFocus mo.Option[string]
}

var (
	// ErrModelUnavailable reports a missing configured model or unsupported reasoning choice.
	ErrModelUnavailable = errors.New("summary model unavailable")
	// ErrCredentialUnavailable reports unavailable credentials for the configured summary model.
	ErrCredentialUnavailable = errors.New("summary model credential unavailable")
	// ErrModelFailed reports a failed or invalid summary-model response.
	ErrModelFailed = errors.New("summary model failed")
)
