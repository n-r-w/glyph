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

// GrepResult contains formatted grep output.
type GrepResult struct {
	// Text contains bounded formatted matches.
	Text string
}

// FindCommand contains find search options.
type FindCommand struct {
	// Pattern contains the file name glob.
	Pattern string
	// Path limits search to one project path.
	Path string
	// Limit is the maximum number of results.
	Limit mo.Option[uint]
}

// FindResult contains formatted find output.
type FindResult struct {
	// Text contains bounded formatted file paths.
	Text string
}

// ListCommand contains directory-listing options.
type ListCommand struct {
	// Path identifies the project directory to list.
	Path string
	// Limit is the maximum number of entries.
	Limit mo.Option[uint]
}

// ListResult contains formatted directory-listing output.
type ListResult struct {
	// Text contains bounded formatted directory entries.
	Text string
}
