// Package cli parses the mutually exclusive headless, RPC, and UI invocation modes.
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
	// ModeRPC starts programmatic control without a UI plugin.
	ModeRPC
)

// Command contains one validated invocation.
type Command struct {
	// Mode identifies the selected Glyph operation mode.
	Mode Mode
	// Headless contains the validated headless command.
	Headless headless.Command
	// ExtensionDirectory overrides the extension catalog directory.
	ExtensionDirectory string
	// UIDirectory overrides the UI catalog directory.
	UIDirectory string
	// UIID identifies an explicitly selected UI plugin.
	UIID string
	// SocketPath overrides the Programmatic Control socket path.
	SocketPath string
}

// Parse validates one Glyph controller invocation.
func Parse(arguments []string) (Command, error) {
	if len(arguments) > 0 {
		switch arguments[0] {
		case "run":
			headlessCommand, err := headless.Parse(arguments)
			if err != nil {
				return Command{}, err
			}
			return Command{
				Mode: ModeHeadless, Headless: headlessCommand,
				ExtensionDirectory: "", UIDirectory: "", UIID: "", SocketPath: "",
			}, nil
		case "rpc":
			return parseRPC(arguments[1:])
		}
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
		SocketPath:         "",
	}, nil
}

// parseRPC accepts only extension and socket configuration for programmatic control.
func parseRPC(arguments []string) (Command, error) {
	flags := flag.NewFlagSet("glyph rpc", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	extensionDirectory := flags.String("extension-dir", "", "replace the extension catalog directory")
	socketPath := flags.String("socket", "", "listen on one Unix socket path")
	if err := flags.Parse(arguments); err != nil {
		return Command{}, fmt.Errorf("parse Glyph RPC arguments: %w", err)
	}
	if flags.NArg() != 0 {
		return Command{}, errors.New("glyph RPC mode does not accept positional input")
	}
	visited := make(map[string]bool)
	flags.Visit(func(parsed *flag.Flag) { visited[parsed.Name] = true })
	if visited["extension-dir"] && strings.TrimSpace(*extensionDirectory) == "" {
		return Command{}, errors.New("--extension-dir requires a nonempty path")
	}
	if visited["socket"] && strings.TrimSpace(*socketPath) == "" {
		return Command{}, errors.New("--socket requires a nonempty path")
	}
	return Command{
		Mode:               ModeRPC,
		Headless:           headless.Command{UserText: "", ExtensionDirectory: ""},
		ExtensionDirectory: *extensionDirectory,
		UIDirectory:        "",
		UIID:               "",
		SocketPath:         *socketPath,
	}, nil
}
