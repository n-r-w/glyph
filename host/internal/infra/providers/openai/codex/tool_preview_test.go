package codex

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

func TestFunctionPreviewAssemblerPublishesCompleteFieldsAndExactScalarPrefix(t *testing.T) {
	t.Parallel()

	assembler := newFunctionPreviewAssembler()
	t.Cleanup(assembler.close)

	fields, err := assembler.appendFragment(`{"path":"file.txt","query":"hel`)
	require.NoError(t, err)
	require.Equal(t, []model.ToolCallPreviewField{
		{Name: "path", Kind: model.ToolCallPreviewFieldComplete, Value: "file.txt", Prefix: ""},
		{Name: "query", Kind: model.ToolCallPreviewFieldPrefix, Value: nil, Prefix: "hel"},
	}, fields)
}

func TestFunctionPreviewAssemblerHidesUntrustworthyPartialValues(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"nested container": `{"options":{"query":"hel`,
		"escaped string":   `{"query":"hel\u12`,
		"incomplete key":   `{"que`,
	}
	for name, fragment := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assembler := newFunctionPreviewAssembler()
			t.Cleanup(assembler.close)

			fields, err := assembler.appendFragment(fragment)
			require.NoError(t, err)
			require.Empty(t, fields)
		})
	}
}
