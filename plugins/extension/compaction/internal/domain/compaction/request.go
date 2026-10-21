package compaction

import (
	"errors"
	"fmt"

	"github.com/samber/mo"
)

// Request contains one original or composed compaction request.
type Request struct {
	// Trigger identifies the compaction intent.
	Trigger Trigger
	// Retry indicates overflow-retry intent.
	Retry bool
	// Guidance contains caller instructions for summarization.
	Guidance string
	// Model describes the model for summary generation.
	Model Model
	// Reasoning contains the reasoning choice for summary generation.
	Reasoning string
	// Prefix contains the entries to summarize.
	Prefix []Entry
	// Suffix contains preserved entries.
	Suffix []Entry
	// Previous contains the preceding persisted replacement.
	Previous mo.Option[Result]
	// ContextTokens is the estimated captured input size.
	ContextTokens int64
	// Estimated identifies content-based accounting.
	Estimated bool
	// Window is the captured conversation capacity.
	Window int64
	// Reserve is the captured response reserve.
	Reserve int64
	// Retained is the configured recent-history target.
	Retained int64
}

// Validate checks the complete compaction request state.
func (r Request) Validate() error {
	if !r.Trigger.Valid() || r.Retry != (r.Trigger == TriggerOverflow) {
		return errors.New("invalid compaction trigger or retry intent")
	}
	if err := r.Model.Validate(); err != nil {
		return err
	}
	if err := r.validateAccounting(); err != nil {
		return err
	}
	if len(r.Prefix) == 0 || len(r.Suffix) == 0 {
		return errors.New("compaction request requires a summarized prefix and retained suffix")
	}
	for _, entries := range [][]Entry{r.Prefix, r.Suffix} {
		for index := range entries {
			entry := &entries[index]
			if entry.ID == "" || entry.SourcePosition < 0 || !entry.SourceKind.Valid() || entry.PreviousSummary ||
				entry.Tokens < 0 {
				return fmt.Errorf("invalid compaction input entry %q", entry.ID)
			}
		}
	}
	return nil
}

// validateAccounting checks the request's estimated input and response reserve.
func (r Request) validateAccounting() error {
	if !r.Estimated || r.ContextTokens < 0 || r.Window <= 0 || r.Reserve <= 0 || r.Reserve > r.Window ||
		r.Retained <= 0 {
		return errors.New("invalid compaction token accounting")
	}
	return nil
}
