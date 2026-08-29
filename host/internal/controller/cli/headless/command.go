package headless

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Command is one validated headless invocation.
type Command struct {
	// UserText contains the submitted user request.
	UserText string
	// ExtensionDirectory overrides the extension catalog directory.
	ExtensionDirectory string
}

// Parse accepts only the prototype `glyph run` command shape.
func Parse(arguments []string) (Command, error) {
	if len(arguments) == 0 || arguments[0] != "run" {
		return Command{}, errors.New("expected command: glyph run [--extension-dir <path>] <request>")
	}
	for _, argument := range arguments[1:] {
		if argument == "--ui" || strings.HasPrefix(argument, "--ui=") ||
			argument == "--ui-dir" || strings.HasPrefix(argument, "--ui-dir=") {
			return Command{}, errors.New("glyph run cannot be combined with --ui or --ui-dir")
		}
	}

	flags := flag.NewFlagSet("glyph run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	extensionDirectory := flags.String("extension-dir", "", "replace the extension catalog directory")
	if err := flags.Parse(arguments[1:]); err != nil {
		return Command{}, fmt.Errorf("parse glyph run arguments: %w", err)
	}
	extensionDirectorySet := false
	flags.Visit(func(parsed *flag.Flag) {
		if parsed.Name == "extension-dir" {
			extensionDirectorySet = true
		}
	})
	if extensionDirectorySet && *extensionDirectory == "" {
		return Command{}, errors.New("--extension-dir requires a nonempty path")
	}
	if flags.NArg() != 1 || strings.TrimSpace(flags.Arg(0)) == "" {
		return Command{}, errors.New("glyph run requires exactly one nonempty text request")
	}
	return Command{UserText: flags.Arg(0), ExtensionDirectory: *extensionDirectory}, nil
}
