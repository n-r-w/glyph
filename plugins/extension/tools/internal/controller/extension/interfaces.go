package extension

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extension

// ReadTool executes the standard read operation after transport validation.
type ReadTool interface {
	Read(ctx context.Context, path string) (string, error)
}

// EditTool executes the standard exact-fragment replacement.
type EditTool interface {
	Edit(ctx context.Context, path, oldText, newText string) error
}

// BashProgressChannel identifies standard command progress.
type BashProgressChannel uint8

const (
	// BashProgressStatus carries lifecycle status.
	BashProgressStatus BashProgressChannel = iota
	// BashProgressStdout carries standard output.
	BashProgressStdout
	// BashProgressStderr carries standard error.
	BashProgressStderr
)

// BashProgress is one command progress fragment.
type BashProgress struct {
	Channel BashProgressChannel
	Content string
}

// BashResult contains complete command output and exit status.
type BashResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// BashTool executes one command and streams progress.
type BashTool interface {
	Execute(ctx context.Context, command string, handleProgress func(BashProgress) error) (BashResult, error)
}
