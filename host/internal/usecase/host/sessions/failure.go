package sessions

import (
	"context"
	"log/slog"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// persistenceOperation is a safe closed classification that contains no session payload.
type persistenceOperation string

const (
	// persistenceOperationHistory identifies a failed history append.
	persistenceOperationHistory persistenceOperation = "append_history"
	// persistenceOperationName identifies a failed session-name mutation.
	persistenceOperationName persistenceOperation = "append_name"
	// persistenceOperationNavigation identifies a failed tree navigation.
	persistenceOperationNavigation persistenceOperation = "navigate"
	// persistenceOperationResume identifies a failed stored-session load.
	persistenceOperationResume persistenceOperation = "resume"
	// persistenceOperationReplacement identifies a failed fork or clone snapshot creation.
	persistenceOperationReplacement persistenceOperation = "create_replacement"
	// persistenceOperationLabel identifies a failed entry-label mutation.
	persistenceOperationLabel persistenceOperation = "set_label"
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
