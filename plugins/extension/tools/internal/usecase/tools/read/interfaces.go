package read

import (
	"context"

	"github.com/samber/mo"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=read

// Image contains image bytes detected from file content.
type Image struct {
	MediaType string
	Data      []byte
}

// Content contains one bounded file read.
type Content struct {
	Text          mo.Option[string]
	Image         mo.Option[Image]
	Start         mo.Option[uint]
	End           mo.Option[uint]
	Total         mo.Option[uint]
	Next          mo.Option[uint]
	OversizedSize mo.Option[int64]
}

// ProjectReader reads bounded content from the working project.
type ProjectReader interface {
	ReadFile(context.Context, string, mo.Option[uint], mo.Option[uint]) (Content, error)
}
