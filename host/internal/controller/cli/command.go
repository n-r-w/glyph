// Package cli parses the mutually exclusive headless and UI invocation modes.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
	"github.com/n-r-w/glyph/host/internal/domain/pluginid"
)

// Mode identifies one Glyph controller mode.
type Mode uint8

const (
	// ModeHeadless runs one request without inspecting a UI catalog.
	ModeHeadless Mode = iota + 1
	// ModeUI starts one selected UI plugin with no positional request.
	ModeUI
)

// Command contains one validated invocation.
type Command struct {
	Mode               Mode
	Headless           headless.Command
	ExtensionDirectory string
	UIDirectory        string
	UIID               string
}

// Parse validates a headless `run` command or a positional-free UI invocation.
func Parse(arguments []string) (Command, error) {
	if len(arguments) > 0 && arguments[0] == "run" {
		headlessCommand, err := headless.Parse(arguments)
		if err != nil {
			return Command{}, err
		}
		return Command{
			Mode: ModeHeadless, Headless: headlessCommand,
			ExtensionDirectory: "", UIDirectory: "", UIID: "",
		}, nil
	}

	flags := flag.NewFlagSet("glyph", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	extensionDirectory := flags.String("extension-dir", "", "replace the extension catalog directory")
	uiDirectory := flags.String("ui-dir", "", "replace the UI catalog directory")
	uiID := flags.String("ui", "", "select one UI plugin")
	if err := flags.Parse(arguments); err != nil {
		return Command{}, fmt.Errorf("parse Glyph UI arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return Command{}, errors.New("glyph UI mode does not accept positional input")
	}
	visited := make(map[string]bool)
	flags.Visit(func(parsed *flag.Flag) { visited[parsed.Name] = true })
	if visited["extension-dir"] && strings.TrimSpace(*extensionDirectory) == "" {
		return Command{}, errors.New("--extension-dir requires a nonempty path")
	}
	if visited["ui-dir"] && strings.TrimSpace(*uiDirectory) == "" {
		return Command{}, errors.New("--ui-dir requires a nonempty path")
	}
	normalizedUIID := pluginid.Normalize(*uiID)
	if visited["ui"] && normalizedUIID == "" {
		return Command{}, errors.New("--ui requires a nonempty normalized plugin ID")
	}
	return Command{
		Mode:               ModeUI,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: *extensionDirectory,
		UIDirectory:        *uiDirectory,
		UIID:               normalizedUIID,
	}, nil
}
