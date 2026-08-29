package read

import (
	"context"

	"github.com/samber/mo"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=read

// Image contains image bytes detected from file content.
type Image struct {
	// MediaType identifies the image format.
	MediaType string
	// Data contains encoded image bytes.
	Data []byte
}

// Content contains one bounded file read.
type Content struct {
	// Text contains bounded file text.
	Text mo.Option[string]
	// Image contains detected image content.
	Image mo.Option[Image]
	// Start is the first returned one-based line.
	Start mo.Option[uint]
	// End is the last returned one-based line.
	End mo.Option[uint]
	// Total is the complete file line count.
	Total mo.Option[uint]
	// Next is the next unread one-based line.
	Next mo.Option[uint]
	// OversizedSize contains one omitted oversized line byte count.
	OversizedSize mo.Option[int64]
}

// ProjectReader reads bounded content from the working project.
type ProjectReader interface {
	ReadFile(context.Context, string, mo.Option[uint], mo.Option[uint]) (Content, error)
}
