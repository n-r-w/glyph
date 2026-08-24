package read

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=read

// Image contains image bytes detected from file content.
type Image struct {
	MediaType string
	Data      []byte
}

// Content contains one bounded file read.
type Content struct {
	Text                    string
	Image                   *Image
	Start, End, Total, Next uint
	OversizedSize           int64
}

// ProjectReader reads bounded content from the working project.
type ProjectReader interface {
	ReadFile(context.Context, string, uint, uint) (Content, error)
}
