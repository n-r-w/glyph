package sessions

import (
	"context"
	"errors"
	"log/slog"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// persistenceOperation is a safe closed classification that contains no session payload.
type persistenceOperation string

const (
	persistenceOperationHistory persistenceOperation = "append_history"
	persistenceOperationName    persistenceOperation = "append_name"
	persistenceOperationResume  persistenceOperation = "resume"
)

// logPersistenceFailure records safe operation, known active identity, and root infrastructure cause.
func logPersistenceFailure(ctx context.Context, operation persistenceOperation, id session.ID, err error) {
	attributes := []any{
		slog.String("operation", string(operation)),
		slog.Any("error", persistenceInfrastructureCause(err)),
	}
	if id != "" {
		attributes = append(attributes, slog.String("session_id", string(id)))
	}
	slog.ErrorContext(ctx, "session persistence failed", attributes...)
}

// persistenceInfrastructureCause removes classification and path-bearing wrappers from diagnostic errors.
func persistenceInfrastructureCause(err error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, nested := range joined.Unwrap() {
			if cause := persistenceInfrastructureCause(nested); cause != nil {
				return cause
			}
		}
		return nil
	}
	if nested := errors.Unwrap(err); nested != nil {
		return persistenceInfrastructureCause(nested)
	}
	if errors.Is(err, session.ErrPersistenceUnavailable) {
		return nil
	}
	return err
}
