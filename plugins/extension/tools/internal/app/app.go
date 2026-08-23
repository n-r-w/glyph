// Package app assembles the standard tools extension process.
package app

import (
	"fmt"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/infra/filesystem/project"
	bashprocess "github.com/n-r-w/glyph/plugins/extension/tools/internal/infra/process/bash"
	bashtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/bash"
	edittool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/edit"
	readtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/read"
	writetool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/write"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Serve assembles the standard tools and serves Extension Contract v1.
func Serve() error {
	projectFiles := project.New()
	readService := readtool.New(projectFiles)
	writeService := writetool.New(projectFiles)
	editService := edittool.New(projectFiles)
	bashService := bashtool.New(bashprocess.New())
	controller, err := extensioncontroller.New(readService, writeService, editService, bashService)
	if err != nil {
		return fmt.Errorf("create extension controller: %w", err)
	}
	extensionsdk.Serve(controller)
	return nil
}
