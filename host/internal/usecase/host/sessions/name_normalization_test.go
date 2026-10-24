//go:build !integration

package sessions

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestSetActiveNameNormalization preserves caller-visible bytes and rejects empty names.
func TestSetActiveNameNormalization(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "delimiters", input: "\r\n\n\r", want: ""},
		{name: "whitespace", input: " \t\u2003\r\n", want: ""},
		{name: "runs", input: "\r\na\r\n\n\rb\n", want: "a b"},
		{name: "spaces tabs", input: " \ta  \tb\r\n \tc \t", want: "a  \tb  \tc"},
		{name: "unicode invalid", input: "\u2003a\u00a0\xff\r\n\xfeb\u2003", want: "a\u00a0\xff \xfeb"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange generated dependencies and the expected durable name.
			ctrl := gomock.NewController(t)
			repository := NewMockRepository(ctrl)
			clock := NewMockClock(ctrl)
			service := New(repository, nil, clock, nil, "/project")
			if test.want != "" {
				clock.EXPECT().Now().Return(time.Time{})
				repository.EXPECT().Apply(gomock.Any(), ApplyCommand{
					Header:      session.Header{},
					StoragePath: "",
					Mutation: Mutation{
						Entry:      mo.None[session.Entry](),
						Navigation: mo.None[NavigationMutation](),
						Label:      mo.None[LabelMutation](),
						SessionInformation: mo.Some(
							SessionInformationMutation{Name: test.want, CreatedAt: time.Time{}},
						),
					},
				}).Return(ApplyResult{StoragePath: ""}, nil)
			}
			// Act through the public name mutation.
			info, err := service.SetActiveName(t.Context(), test.input)
			// Assert exact returned bytes or the unchanged invalid-name error.
			if test.want == "" {
				require.ErrorIs(t, err, session.ErrInvalidName)
			} else {
				require.NoError(t, err)
				require.Equal(t, mo.Some(test.want), info.Name)
				require.Equal(t, info, service.ActiveInfo())
			}
		})
	}
}
