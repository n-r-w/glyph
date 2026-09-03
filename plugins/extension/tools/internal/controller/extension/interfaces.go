package extension

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extension

// ReadImage contains image bytes detected from file content.
type ReadImage struct {
	// MediaType identifies the image format.
	MediaType string
	// Data contains encoded image bytes.
	Data []byte
}

// ReadResult contains a text or image read result.
type ReadResult struct {
	// Text contains bounded file text.
	Text mo.Option[string]
	// Image contains detected image content.
	Image mo.Option[ReadImage]
}

// ReadTool executes bounded reads.
type ReadTool interface {
	Read(context.Context, string, mo.Option[uint], mo.Option[uint]) (ReadResult, error)
}

// WriteTool replaces one project file.
type WriteTool interface {
	Write(context.Context, string, string) error
}

// Replacement identifies one exact source replacement.
type Replacement struct {
	// OldText identifies the exact source text to replace.
	OldText string `json:"oldText"`
	// NewText contains replacement text.
	NewText string `json:"newText"`
}

// EditTool applies replacements to one project file.
type EditTool interface {
	Edit(context.Context, string, []Replacement) error
}

// BashProgressChannel identifies command progress.
type BashProgressChannel uint8

const (
	// BashProgressStatus carries command lifecycle state.
	BashProgressStatus BashProgressChannel = iota
	// BashProgressStdout carries standard output.
	BashProgressStdout
	// BashProgressStderr carries standard error.
	BashProgressStderr
)

// BashProgress is one command output fragment.
type BashProgress struct {
	// Channel identifies the progress fragment meaning.
	Channel BashProgressChannel
	// Content contains the progress fragment text.
	Content string
}

// BashResult contains bounded command output, exit status, and truncation metadata.
type BashResult struct {
	// Text contains bounded model-visible command output.
	Text string
	// ExitCode contains the command process exit status.
	ExitCode int
	// Truncation describes omitted complete output.
	Truncation textbudget.Truncation
}

// BashTool executes one command.
type BashTool interface {
	Execute(context.Context, string, func(BashProgress) error) (BashResult, error)
}

// GrepArguments contains validated grep input.
type GrepArguments struct {
	// Pattern contains the search expression.
	Pattern string
	// Path limits search to one project path.
	Path string
	// Glob limits search to matching project files.
	Glob string
	// IgnoreCase enables case-insensitive matching.
	IgnoreCase bool
	// Literal treats Pattern as literal text.
	Literal bool
	// Context is the number of surrounding lines to include.
	Context uint
	// Limit is the maximum number of matches.
	Limit mo.Option[uint]
}

// FindArguments contains validated find input.
type FindArguments struct {
	// Pattern contains the file name glob.
	Pattern string
	// Path limits search to one project path.
	Path string
	// Limit is the maximum number of results.
	Limit mo.Option[uint]
}

// ListArguments contains validated ls input.
type ListArguments struct {
	// Path identifies the project directory to list.
	Path string
	// Limit is the maximum number of entries.
	Limit mo.Option[uint]
}

// SearchTool executes the project discovery tools.
type SearchTool interface {
	Grep(context.Context, GrepArguments) (string, error)
	Find(context.Context, FindArguments) (string, error)
	List(context.Context, ListArguments) (string, error)
}
