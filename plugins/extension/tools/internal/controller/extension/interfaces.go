package extension

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extension

// ReadImage contains image bytes detected from file content.
type ReadImage struct {
	MediaType string
	Data      []byte
}

// ReadResult contains a text or image read result.
type ReadResult struct {
	Text  string
	Image *ReadImage
}

// ReadTool executes bounded reads.
type ReadTool interface {
	Read(context.Context, string, uint, uint) (ReadResult, error)
}

// WriteTool replaces one project file.
type WriteTool interface {
	Write(context.Context, string, string) error
}

// Replacement identifies one exact source replacement.
type Replacement struct {
	OldText string `json:"oldText"`
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
	Channel BashProgressChannel
	Content string
}

// BashResult contains command output and exit status.
type BashResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// BashTool executes one command.
type BashTool interface {
	Execute(context.Context, string, func(BashProgress) error) (BashResult, error)
}
