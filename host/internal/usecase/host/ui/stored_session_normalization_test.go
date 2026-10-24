//go:build !integration

package ui

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestStoredSessionPreviewNormalization preserves preview bytes and optional presence.
func TestStoredSessionPreviewNormalization(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input mo.Option[string]
		want  mo.Option[string]
	}{
		{name: "absent", input: mo.None[string](), want: mo.None[string]()},
		{name: "empty", input: mo.Some(""), want: mo.Some("")},
		{name: "delimiters", input: mo.Some("\r\n\n\r"), want: mo.Some("")},
		{name: "runs", input: mo.Some("\r\na\r\n\n\rb\n"), want: mo.Some("a b")},
		{name: "spaces tabs", input: mo.Some(" \ta  \tb\r\n \tc \t"), want: mo.Some("a  \tb  \tc")},
		{name: "unicode invalid", input: mo.Some("\u2003a\u00a0\xff\r\n\xfeb\u2003"), want: mo.Some("a\u00a0\xff \xfeb")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange stored text independently of session metadata.
			stored := StoredSession{Info: session.Info{}, FirstUserText: test.input, TotalMessages: 0}
			// Act through the client list projection.
			got := stored.publicItem()
			// Assert exact bytes and presence in the public result.
			require.Equal(t, test.want, got.FirstUserText)
		})
	}
}
