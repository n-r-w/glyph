// Package sessiontree coordinates internal session-tree navigation.
package sessiontree

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessiontree

// ActiveSession exposes the tree snapshot and atomic navigation commit needed by navigation.
type ActiveSession interface {
	// Tree returns an independent active-session tree snapshot.
	Tree() session.Tree
	// CommitNavigation persists one destination change when the expected active leaf is still current.
	CommitNavigation(context.Context, mo.Option[string], mo.Option[string]) (session.Tree, error)
}
