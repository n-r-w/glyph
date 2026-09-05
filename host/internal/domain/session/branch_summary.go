package session

import (
	"errors"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// BranchSummarySource identifies the producer of a summary and its model usage.
type BranchSummarySource struct {
	// ExtensionID identifies a producer that did not execute a model.
	ExtensionID mo.Option[string]
	// Model contains the actual model selection and reported usage.
	Model mo.Option[BranchSummaryModelSource]
}

// BranchSummaryModelSource keeps model identity and usage in one source alternative.
type BranchSummaryModelSource struct {
	// Selection identifies the model execution that produced this result.
	Selection model.Selection
	// Usage contains normalized tokens when reported by the producer.
	Usage mo.Option[TokenUsage]
}

// Validate checks the exclusive source shape and reported model usage.
func (source BranchSummarySource) Validate() error {
	if source.ExtensionID.IsSome() == source.Model.IsSome() {
		return errors.New("branch summary requires exactly one source")
	}
	if extensionID, present := source.ExtensionID.Get(); present {
		if strings.TrimSpace(extensionID) == "" {
			return errors.New("branch summary extension source is empty")
		}
		return nil
	}
	// The exclusive source check guarantees model presence after the extension branch returns.
	modelSource, _ := source.Model.Get()
	// Historical identity must remain usable without the producing model's configuration.
	selection := modelSource.Selection
	if strings.TrimSpace(string(selection.Provider)) == "" || strings.TrimSpace(string(selection.Model)) == "" ||
		!selection.ReasoningChoice.Valid() {
		return errors.New("branch summary model source is invalid")
	}
	if usage, present := modelSource.Usage.Get(); present && !usage.Valid() {
		return errors.New("branch summary model usage is invalid")
	}
	return nil
}
