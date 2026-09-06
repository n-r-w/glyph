//go:build !integration

package ui

import (
	"os"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestSelectorRejectsInvalidCatalogueBeforeStarting preserves whole-catalog UI acceptance.
func TestSelectorRejectsInvalidCatalogueBeforeStarting(t *testing.T) {
	t.Parallel()
	// Arrange each rejected observation with a valid explicitly selected candidate.
	cause := &os.PathError{Op: "stat", Path: "/ui/broken", Err: os.ErrPermission}
	cases := []struct {
		// name identifies the acceptance failure.
		name string
		// discovery supplies filesystem observations.
		discovery Discovery
		// message identifies the established diagnostic context.
		message string
		// cause is the filesystem cause that must survive when present.
		cause error
	}{
		{
			name:      "directory",
			discovery: Discovery{Candidates: nil, Failures: nil, DirectoryError: cause},
			message:   "read UI directory",
			cause:     cause,
		},
		{
			name: "entry",
			discovery: Discovery{
				Candidates:     []Candidate{{ID: "valid", Path: "/ui/valid"}},
				Failures:       []CandidateFailure{{Name: "broken", Err: cause}},
				DirectoryError: nil,
			},
			message: "inspect UI candidate",
			cause:   cause,
		},
		{
			name: "empty identity",
			discovery: Discovery{
				Candidates:     []Candidate{{ID: "valid", Path: "/ui/valid"}, {ID: "", Path: "/ui/___"}},
				Failures:       nil,
				DirectoryError: nil,
			},
			message: "empty normalized ID",
			cause:   nil,
		},
		{
			name: "duplicate identity",
			discovery: Discovery{
				Candidates: []Candidate{
					{ID: "valid", Path: "/ui/valid"},
					{ID: "duplicate", Path: "/ui/Duplicate"},
					{ID: "duplicate", Path: "/ui/duplicate"},
				},
				Failures:       nil,
				DirectoryError: nil,
			},
			message: "duplicate normalized ID",
			cause:   nil,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange dependencies with no process-start expectation.
			controller := gomock.NewController(t)
			catalog := NewMockCatalog(controller)
			runtime := NewMockRuntime(controller)
			directory := Directory{Path: "/ui"}
			catalog.EXPECT().Discover(t.Context(), directory).Return(test.discovery, nil)
			// Act through full selection rather than the private acceptance helper.
			_, err := NewSelector(
				catalog,
				runtime,
			).Select(t.Context(), SelectionRequest{Directory: directory, ExplicitUI: "valid", ActiveUI: mo.None[string]()})
			// Assert that acceptance rejects before a process can start and retains the cause.
			require.ErrorContains(t, err, test.message)
			if test.cause != nil {
				require.ErrorIs(t, err, test.cause)
				require.ErrorContains(t, err, test.cause.Error())
			}
		})
	}
}
