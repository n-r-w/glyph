//go:build !integration

package programmatic

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestCompactionFailuresKeepPublicCategories verifies Programmatic failure mapping keeps owner identity.
func TestCompactionFailuresKeepPublicCategories(t *testing.T) {
	t.Parallel()
	// Arrange realistic orchestration and persistence terminal failures.
	cases := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "compaction",
			err:      compactionCategoryError{code: controller.FailureCodeCompactionFailed},
			expected: controller.FailureCodeCompactionFailed,
		},
		{
			name:     "extension",
			err:      compactionCategoryError{code: controller.FailureCodeExtensionFailed},
			expected: controller.FailureCodeExtensionFailed,
		},
		{
			name:     "persistence",
			err:      session.ErrPersistenceUnavailable,
			expected: controller.FailureCodePersistenceUnavailable,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Act and assert the transport receives the original closed category.
			require.Equal(t, testCase.expected, failureCode(testCase.err))
		})
	}
}

// compactionCategoryError supplies one stable test-only orchestration category.
type compactionCategoryError struct {
	// code is the stable public failure category.
	code string
}

// Error returns the category as complete test failure text.
func (e compactionCategoryError) Error() string { return e.code }

// CompactionFailureCode returns the orchestration-owned category.
func (e compactionCategoryError) CompactionFailureCode() string { return e.code }

// TestManualCompactionPreservesCommittedFailureCategory verifies production Programmatic operation mapping.
func TestManualCompactionPreservesCommittedFailureCategory(t *testing.T) {
	t.Parallel()
	// Arrange post-commit publication, observer, and joined failures.
	publication := errors.New("publication failed after commit")
	observer := errors.New("observer failed after commit")
	tests := []struct {
		name         string
		failure      error
		expectedCode string
	}{
		{
			name:         "publication",
			failure:      programmaticPostCommitFailure{code: controller.FailureCodeInternal, cause: publication},
			expectedCode: controller.FailureCodeInternal,
		},
		{
			name:         "observer",
			failure:      programmaticPostCommitFailure{code: controller.FailureCodeExtensionFailed, cause: observer},
			expectedCode: controller.FailureCodeExtensionFailed,
		},
		{name: "joined", failure: errors.Join(
			programmaticPostCommitFailure{code: controller.FailureCodeInternal, cause: publication},
			programmaticPostCommitFailure{code: controller.FailureCodeExtensionFailed, cause: observer},
		), expectedCode: controller.FailureCodeInternal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			mockController := gomock.NewController(t)
			compactor := NewMockCompactor(mockController)
			committed := session.Entry{ID: "compaction", Compaction: mo.Some(session.CompactionEntry{
				Summary: "summary", FirstKeptEntryID: "kept",
				Source: session.CompactionSource{
					ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
				},
				EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
			})}
			compactor.EXPECT().CompactProgrammatic(gomock.Any(), mo.None[string]()).Return(
				ManualCompactionResult{Committed: mo.Some(committed), Canceled: false}, testCase.failure,
			)
			service := New(nil, nil, testStateQuery(t, false), nil, nil, nil, testRunOutput(t), nil, nil)
			require.NoError(t, service.BindCompactor(compactor))
			command := testProgrammaticCommand("compact", controller.CommandCompact)

			// Act through the production Programmatic command path.
			response, active, err := service.handle(t.Context(), command)

			// Assert committed state, complete text, and stable category survive together.
			require.Nil(t, active)
			require.ErrorIs(t, err, testCase.failure)
			require.Len(t, response.SessionEntries, 1)
			require.Equal(t, testCase.failure.Error(), response.CompactionError.MustGet())
			code, codePresent := response.CompactionFailureCode.Get()
			require.True(t, codePresent)
			require.Equal(t, testCase.expectedCode, code)
		})
	}
}

// programmaticPostCommitFailure keeps a stable category and complete post-commit cause.
type programmaticPostCommitFailure struct {
	// code is the stable public category.
	code string
	// cause is the complete underlying failure.
	cause error
}

// Error returns the complete underlying failure text.
func (e programmaticPostCommitFailure) Error() string { return e.cause.Error() }

// Unwrap preserves the underlying failure.
func (e programmaticPostCommitFailure) Unwrap() error { return e.cause }

// CompactionFailureCode returns the stable category.
func (e programmaticPostCommitFailure) CompactionFailureCode() string { return e.code }

// TestProgrammaticCompactionCancellationMatrix verifies owner cancellation and typed compaction precedence.
func TestProgrammaticCompactionCancellationMatrix(t *testing.T) {
	t.Parallel()
	independentCause := errors.New("independent Programmatic compaction failure")
	for _, testCase := range []struct {
		name                 string
		cancelOwner          bool
		compactionErr        error
		expectedCancellation bool
	}{
		{
			name: "active owner with handler transport cancellation", cancelOwner: false,
			compactionErr: programmaticPostCommitFailure{
				code: controller.FailureCodeExtensionFailed, cause: context.Canceled,
			},
			expectedCancellation: false,
		},
		{
			name: "canceled owner with cancellation only", cancelOwner: true,
			compactionErr: context.Canceled, expectedCancellation: true,
		},
		{
			name: "canceled owner with typed handler cancellation", cancelOwner: true,
			compactionErr: programmaticPostCommitFailure{
				code: controller.FailureCodeExtensionFailed, cause: context.Canceled,
			},
			expectedCancellation: false,
		},
		{
			name: "canceled owner with independent typed failure", cancelOwner: true,
			compactionErr: programmaticPostCommitFailure{
				code:  controller.FailureCodeExtensionFailed,
				cause: errors.Join(context.Canceled, independentCause),
			},
			expectedCancellation: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Arrange the Programmatic operation owner context.
			ctx, cancel := context.WithCancel(t.Context())
			if testCase.cancelOwner {
				cancel()
			} else {
				t.Cleanup(cancel)
			}

			// Act through the production Programmatic terminal cancellation guard.
			canceled := isOperationCancellation(ctx, testCase.compactionErr)

			// Assert only an untyped owning cancellation is converted to operation cancellation.
			require.Equal(t, testCase.expectedCancellation, canceled)
			if !testCase.expectedCancellation {
				require.Equal(t, controller.FailureCodeExtensionFailed, failureCode(testCase.compactionErr))
			}
			if testCase.name == "canceled owner with independent typed failure" {
				require.ErrorIs(t, testCase.compactionErr, independentCause)
			}
		})
	}
}

// TestManualCompactionReturnsCommittedState verifies Programmatic compaction preserves instructions and durable output.
func TestManualCompactionReturnsCommittedState(t *testing.T) {
	t.Parallel()
	// Arrange one bound compaction owner and committed marker.
	mockController := gomock.NewController(t)
	compactor := NewMockCompactor(mockController)
	committed := session.Entry{ID: "compaction", Compaction: mo.Some(session.CompactionEntry{
		Summary: "summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
	})}
	compactor.EXPECT().CompactProgrammatic(gomock.Any(), mo.Some("preserve decisions")).Return(
		ManualCompactionResult{Committed: mo.Some(committed), Canceled: false}, nil,
	)
	service := New(nil, nil, testStateQuery(t, false), nil, nil, nil, testRunOutput(t), nil, nil)
	require.NoError(t, service.BindCompactor(compactor))
	command := testProgrammaticCommand("compact", controller.CommandCompact)
	command.CompactionInstructions = mo.Some("preserve decisions")

	// Act through the transport-independent command path.
	response, active, err := service.handle(t.Context(), command)

	// Assert one terminal compaction response retains the committed marker.
	require.NoError(t, err)
	require.Nil(t, active)
	require.Equal(t, controller.ResponseCompaction, response.Kind)
	require.Len(t, response.SessionEntries, 1)
	require.Equal(t, "compaction", response.SessionEntries[0].ID)
	require.Equal(t, mo.Some(false), response.CompactionCanceled)
}
