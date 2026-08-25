// Package search implements bounded project discovery tools.
package search

import (
	"context"

	"github.com/samber/mo"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=search

// ProjectFiles performs bounded project filesystem operations.
type ProjectFiles interface {
	Grep(context.Context, GrepCommand) (GrepResult, error)
	Find(context.Context, FindCommand) (FindResult, error)
	List(context.Context, ListCommand) (ListResult, error)
}

// GrepCommand contains grep search options.
type GrepCommand struct {
	Pattern    string
	Path       string
	Glob       string
	IgnoreCase bool
	Literal    bool
	Context    uint
	Limit      mo.Option[uint]
}

// GrepResult contains formatted grep output.
type GrepResult struct{ Text string }

// FindCommand contains find search options.
type FindCommand struct {
	Pattern string
	Path    string
	Limit   mo.Option[uint]
}

// FindResult contains formatted find output.
type FindResult struct{ Text string }

// ListCommand contains directory-listing options.
type ListCommand struct {
	Path  string
	Limit mo.Option[uint]
}

// ListResult contains formatted directory-listing output.
type ListResult struct{ Text string }
