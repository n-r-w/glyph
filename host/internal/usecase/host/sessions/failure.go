package sessions

import (
	"context"
	"log/slog"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// persistenceOperation is a safe closed classification that contains no session payload.
type persistenceOperation string

const (
	persistenceOperationHistory    persistenceOperation = "append_history"
	persistenceOperationName       persistenceOperation = "append_name"
	persistenceOperationNavigation persistenceOperation = "navigate"
	persistenceOperationResume     persistenceOperation = "resume"
)

// logPersistenceFailure records the operation, active identity, and complete error chain.
func logPersistenceFailure(ctx context.Context, operation persistenceOperation, id session.ID, err error) {
	attributes := []any{
		slog.String("operation", string(operation)),
		slog.Any("error", err),
	}
	if id != "" {
		attributes = append(attributes, slog.String("session_id", string(id)))
	}
	slog.ErrorContext(ctx, "session persistence failed", attributes...)
}
