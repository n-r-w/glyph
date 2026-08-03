package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/go-plugin"
)

const (
	modeCheckAutomated = "check-automated"
	modeCheckTerminal  = "check-terminal"
	modeCheckAll       = "check-all"
	modeServeExtension = "serve-extension"
	modeServeUI        = "serve-ui"
)

// main maps process exit status to one actionable spike result.
func main() {
	if err := runMain(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// runMain dispatches Host checks or one child plugin server role.
func runMain() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: plugin-runtime-spike <check-automated|check-terminal|check-all>")
	}

	switch os.Args[1] {
	case modeServeExtension:
		return serveExtensionProcess(os.Args[2:])
	case modeServeUI:
		return serveUIProcess(os.Args[2:])
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve spike executable: %w", err)
	}

	switch os.Args[1] {
	case modeCheckAutomated:
		report, err := runAutomatedChecks(ctx, executable)
		if err != nil {
			return err
		}
		if !report.passed() {
			return fmt.Errorf("automated runtime outcomes incomplete: %+v", report)
		}
		fmt.Println("PASS: protocol v1, multiple extensions, streaming, cancellation, crash isolation, collision cleanup, and bidirectional UI")
		return nil
	case modeCheckTerminal:
		return printTerminalResult(ctx, executable)
	case modeCheckAll:
		report, err := runAutomatedChecks(ctx, executable)
		if err != nil {
			return err
		}
		if !report.passed() {
			return fmt.Errorf("automated runtime outcomes incomplete: %+v", report)
		}
		terminal, err := runTerminalChecks(ctx, executable)
		if err != nil {
			return err
		}
		if !terminal.passed() {
			return fmt.Errorf("terminal outcomes incomplete: %+v", terminal)
		}
		fmt.Println("PASS: plugin SDK, multiple extensions, bidirectional UI, cancellation, crash isolation, and terminal restoration")
		return nil
	default:
		return fmt.Errorf("unknown mode %q", os.Args[1])
	}
}

// printTerminalResult prints the observed terminal outcome or returns a precise failure.
func printTerminalResult(ctx context.Context, executable string) error {
	report, err := runTerminalChecks(ctx, executable)
	if err != nil {
		return err
	}
	if !report.passed() {
		return fmt.Errorf("terminal outcomes incomplete: %+v", report)
	}
	fmt.Println("PASS: controlling terminal, resize delivery, normal restoration, and crash restoration")
	return nil
}

// serveExtensionProcess starts one fixed-catalog extension plugin process.
func serveExtensionProcess(arguments []string) error {
	if len(arguments) < 2 {
		return fmt.Errorf("serve extension: expected extension ID and at least one tool")
	}
	service := &extensionService{
		extensionID: arguments[0],
		tools:       arguments[1:],
	}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig:  extensionHandshake(),
		VersionedPlugins: extensionPluginSets(service),
		GRPCServer:       plugin.DefaultGRPCServer,
	})
	return nil
}

// serveUIProcess starts one UI plugin process without opening the terminal before its stream.
func serveUIProcess(arguments []string) error {
	if len(arguments) != 1 {
		return fmt.Errorf("serve UI: expected terminal or headless mode")
	}
	terminal := false
	switch arguments[0] {
	case "headless":
	case "terminal":
		terminal = true
	default:
		return fmt.Errorf("serve UI: unknown mode %q", arguments[0])
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: uiHandshake(),
		VersionedPlugins: uiPluginSets(&uiService{
			terminal: terminal,
		}),
		GRPCServer: plugin.DefaultGRPCServer,
	})
	return nil
}
