package session

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// CompactionSource contains attribution to the extension or model requests that produced a summary.
type CompactionSource struct {
	// ExtensionID identifies a producer that used no model requests.
	ExtensionID mo.Option[string]
	// Requests contains contributing model requests in execution order.
	Requests []CompactionModelRequest
}

// Validate checks producer identity and request accounting.
func (source CompactionSource) Validate() error {
	if source.ExtensionID.IsSome() == (len(source.Requests) > 0) {
		return errors.New("compaction requires an extension source or contributing model requests")
	}
	if extensionID, present := source.ExtensionID.Get(); present {
		if strings.TrimSpace(extensionID) == "" {
			return errors.New("compaction extension source is empty")
		}
	}
	for index := range source.Requests {
		request := &source.Requests[index]
		if err := request.Validate(); err != nil {
			return fmt.Errorf("compaction request %d: %w", index, err)
		}
	}
	return nil
}

// Clone returns independently owned request accounting.
func (source CompactionSource) Clone() CompactionSource {
	source.Requests = slices.Clone(source.Requests)
	return source
}

// CompactionModelRequest contains accounting for one contributing model request.
type CompactionModelRequest struct {
	// Selection identifies the actual model and reasoning choice.
	Selection model.Selection
	// Usage contains normalized reported token counts.
	Usage mo.Option[TokenUsage]
	// EstimatedCost contains the estimated cost of the request.
	EstimatedCost mo.Option[EstimatedCost]
}

// Validate checks model identity, usage, and saved cost.
func (request CompactionModelRequest) Validate() error {
	selection := request.Selection
	if strings.TrimSpace(string(selection.Provider)) == "" || strings.TrimSpace(string(selection.Model)) == "" ||
		!selection.ReasoningChoice.Valid() {
		return errors.New("compaction model selection is invalid")
	}
	if usage, present := request.Usage.Get(); present && !usage.Valid() {
		return errors.New("compaction model usage is invalid")
	}
	if cost, present := request.EstimatedCost.Get(); present {
		if request.Usage.IsNone() {
			return errors.New("compaction cost requires reported model usage")
		}
		if !cost.Valid() {
			return errors.New("compaction estimated cost is invalid")
		}
	}
	return nil
}

// CompactionEntry contains one persisted active-context replacement.
type CompactionEntry struct {
	// Summary contains the replacement context supplied by its source.
	Summary string
	// FirstKeptEntryID identifies the first original entry retained after the summary.
	FirstKeptEntryID string
	// Source contains attribution to the summary's contributing requests or extension.
	Source CompactionSource
	// Details contains optional opaque extension-owned result data.
	Details mo.Option[[]byte]
}

// Clone returns an independently owned compaction payload.
func (compaction CompactionEntry) Clone() CompactionEntry {
	compaction.Source = compaction.Source.Clone()
	compaction.Details = compaction.Details.MapValue(bytes.Clone)
	return compaction
}
