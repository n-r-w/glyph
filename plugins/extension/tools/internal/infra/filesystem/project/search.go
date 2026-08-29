package project

import (
	"strings"

	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

const (
	grepLineCharacters = 500
	directoryBatchSize = 128
	grepDefaultLimit   = 100
	findDefaultLimit   = 1000
	listDefaultLimit   = 500
)

var _ searchtool.ProjectFiles = (*Service)(nil)

// text joins the retained complete lines and notices.
func (o *searchOutput) text() string { return strings.Join(o.lines, "") }
