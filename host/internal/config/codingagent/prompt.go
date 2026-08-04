// Package codingagent owns the prototype's static coding instructions.
package codingagent

import (
	_ "embed"
	"strings"
)

//go:embed prompt.txt
var prompt string

// Instructions returns the resolved English coding-agent prompt.
func Instructions() string {
	return strings.TrimSpace(prompt)
}
