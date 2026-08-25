package programmatic

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// Coordinator owns Host run identifiers, execution, and settlement.
type Coordinator interface {
	PrepareRun() (string, error)
	RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error)
}

// SelectionCode identifies a model catalog selection failure.
type SelectionCode string

const (
	// SelectionNotFound reports an unknown provider and model pair.
	SelectionNotFound SelectionCode = "not_found"
	// SelectionReasoningUnsupported reports an unsupported reasoning level.
	SelectionReasoningUnsupported SelectionCode = "reasoning_unsupported"
	// SelectionCredentialUnavailable reports unavailable selection credentials.
	SelectionCredentialUnavailable SelectionCode = "credential_unavailable" //nolint:gosec // This is an error code.
)

// SelectionFailure exposes a stable safe catalog failure.
type SelectionFailure interface {
	error
	SelectionCode() string
}

// ModelCatalog provides configured models and runtime selection operations.
type ModelCatalog interface {
	Models() []model.Descriptor
	Selection() model.Selection
	SelectModel(ctx context.Context, provider model.ProviderID, modelID model.ID) (model.Selection, error)
	SelectReasoningChoice(level model.ReasoningChoice) (model.Selection, error)
}
