//go:build integration

package sessions

import "github.com/n-r-w/glyph/host/internal/domain/model"

// testToolCallArguments returns validated JSON for a static test fixture.
func testToolCallArguments(value string) model.ToolCallArguments {
	arguments, err := model.NewToolCallArguments([]byte(value))
	if err != nil {
		panic(err)
	}
	return arguments
}
