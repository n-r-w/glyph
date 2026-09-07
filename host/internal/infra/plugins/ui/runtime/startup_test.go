//go:build !integration

package runtime

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

//go:generate go tool mockgen -destination=writer_mock_test.go -package=runtime io Writer

// TestCloseFlushesPendingSelectionWarningsOnce preserves fallback output without duplicate retries.
func TestCloseFlushesPendingSelectionWarningsOnce(t *testing.T) {
	t.Parallel()
	// Arrange two pending exclusions with complete source text.
	var writer strings.Builder
	service := New()
	service.BindSelection(hostui.Selection{ID: "selected", Issues: []hostui.SelectionIssue{
		{Candidate: hostui.Candidate{ID: "first", Path: "/first"}, Err: errors.New("first complete cause")},
		{Candidate: hostui.Candidate{ID: "second", Path: "/second"}, Err: errors.New("second complete cause")},
	}}, &writer)
	// Act through repeated output close calls.
	require.NoError(t, service.Close())
	first := writer.String()
	require.NoError(t, service.Close())
	// Assert both complete causes occur once and later close cannot repeat warnings.
	require.Equal(t, first, writer.String())
	require.Equal(t, 1, strings.Count(first, "first complete cause"))
	require.Equal(t, 1, strings.Count(first, "second complete cause"))
}

// TestClosePreservesAllWarningWriterFailures joins full failures and recognizes a short write.
func TestClosePreservesAllWarningWriterFailures(t *testing.T) {
	t.Parallel()
	// Arrange a failing writer followed by a short write for the second warning.
	writer := NewMockWriter(gomock.NewController(t))
	cause := errors.New("complete writer failure suffix")
	firstSource := errors.New("first excluded candidate source")
	secondSource := errors.New("second excluded candidate source")
	gomock.InOrder(
		writer.EXPECT().Write(gomock.Any()).Return(0, cause),
		writer.EXPECT().Write(gomock.Any()).Return(0, nil),
	)
	service := New()
	service.BindSelection(hostui.Selection{ID: "", Issues: []hostui.SelectionIssue{
		{Candidate: hostui.Candidate{ID: "first", Path: "/first"}, Err: firstSource},
		{Candidate: hostui.Candidate{ID: "second", Path: "/second"}, Err: secondSource},
	}}, writer)
	// Act through the fallback close owner.
	err := service.Close()
	// Assert shutdown can join every original writer cause with the retained context.
	require.ErrorIs(t, err, firstSource)
	require.ErrorIs(t, err, secondSource)
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, io.ErrShortWrite)
	require.ErrorContains(t, err, "write CLI warning: complete writer failure suffix")
	require.NoError(t, service.Close())
}
