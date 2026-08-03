package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
)

const (
	pluginStartTimeout = 10 * time.Second
	processPollDelay   = 10 * time.Millisecond
)

// extensionRuntime owns one extension process and its private transport adapter.
type extensionRuntime struct {
	id      string
	process *plugin.Client
	client  *extensionRPCClient
	tools   []string
	version int
}

// close stops the extension process and waits for go-plugin cleanup.
func (r *extensionRuntime) close() {
	r.process.Kill()
}

// exited reports whether go-plugin observed extension process completion.
func (r *extensionRuntime) exited() bool {
	return r.process.Exited()
}

// uiRuntime owns one UI plugin process and its private transport adapter.
type uiRuntime struct {
	process      *plugin.Client
	client       *uiRPCClient
	version      int
	usesTerminal bool
}

// close stops the UI plugin process and waits for go-plugin cleanup.
func (r *uiRuntime) close() {
	r.process.Kill()
}

// exited reports whether go-plugin observed UI process completion.
func (r *uiRuntime) exited() bool {
	return r.process.Exited()
}

// processID returns the UI plugin operating-system process ID.
func (r *uiRuntime) processID() (int, error) {
	processID, err := strconv.Atoi(r.process.ID())
	if err != nil {
		return 0, fmt.Errorf("parse UI process ID %q: %w", r.process.ID(), err)
	}
	return processID, nil
}

// startExtension launches one versioned extension process and dispenses its transport adapter.
func startExtension(
	ctx context.Context,
	executable string,
	extensionID string,
	tools ...string,
) (*extensionRuntime, error) {
	arguments := append([]string{"serve-extension", extensionID}, tools...)
	process := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  extensionHandshake(),
		VersionedPlugins: extensionPluginSets(nil),
		Cmd:              exec.Command(executable, arguments...),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		StartTimeout:     pluginStartTimeout,
		Logger:           hclog.NewNullLogger(),
		Stderr:           io.Discard,
		SyncStdout:       io.Discard,
		SyncStderr:       io.Discard,
	})

	runtime, err := connectExtension(ctx, process, extensionID)
	if err != nil {
		process.Kill()
		return nil, err
	}
	return runtime, nil
}

// connectExtension completes handshake and type-checks the dispensed extension adapter.
func connectExtension(
	ctx context.Context,
	process *plugin.Client,
	extensionID string,
) (*extensionRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start extension %s: %w", extensionID, err)
	}

	protocolClient, err := process.Client()
	if err != nil {
		return nil, fmt.Errorf("start extension %s: %w", extensionID, err)
	}
	if process.NegotiatedVersion() != protocolVersion {
		return nil, fmt.Errorf(
			"start extension %s: negotiated protocol %d",
			extensionID,
			process.NegotiatedVersion(),
		)
	}

	dispensed, err := protocolClient.Dispense(extensionPluginName)
	if err != nil {
		return nil, fmt.Errorf("dispense extension %s: %w", extensionID, err)
	}
	client, ok := dispensed.(*extensionRPCClient)
	if !ok {
		return nil, fmt.Errorf("dispense extension %s: unexpected adapter %T", extensionID, dispensed)
	}
	return &extensionRuntime{
		id:      extensionID,
		process: process,
		client:  client,
		version: process.NegotiatedVersion(),
	}, nil
}

// startUI launches one versioned UI process and dispenses its transport adapter.
func startUI(ctx context.Context, executable string, terminal bool) (*uiRuntime, error) {
	terminalMode := "headless"
	if terminal {
		terminalMode = "terminal"
	}
	process := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  uiHandshake(),
		VersionedPlugins: uiPluginSets(nil),
		Cmd:              exec.Command(executable, "serve-ui", terminalMode),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		StartTimeout:     pluginStartTimeout,
		Logger:           hclog.NewNullLogger(),
		Stderr:           io.Discard,
		SyncStdout:       io.Discard,
		SyncStderr:       io.Discard,
	})

	runtime, err := connectUI(ctx, process)
	if err != nil {
		process.Kill()
		return nil, err
	}
	return runtime, nil
}

// connectUI completes handshake and type-checks the dispensed UI adapter without opening its stream.
func connectUI(ctx context.Context, process *plugin.Client) (*uiRuntime, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start UI plugin: %w", err)
	}

	protocolClient, err := process.Client()
	if err != nil {
		return nil, fmt.Errorf("start UI plugin: %w", err)
	}
	if process.NegotiatedVersion() != protocolVersion {
		return nil, fmt.Errorf("start UI plugin: negotiated protocol %d", process.NegotiatedVersion())
	}

	dispensed, err := protocolClient.Dispense(uiPluginName)
	if err != nil {
		return nil, fmt.Errorf("dispense UI plugin: %w", err)
	}
	client, ok := dispensed.(*uiRPCClient)
	if !ok {
		return nil, fmt.Errorf("dispense UI plugin: unexpected adapter %T", dispensed)
	}
	usesTerminal, err := client.describe(ctx)
	if err != nil {
		return nil, err
	}
	return &uiRuntime{
		process:      process,
		client:       client,
		version:      process.NegotiatedVersion(),
		usesTerminal: usesTerminal,
	}, nil
}

// waitForExit waits until go-plugin observes process completion or context cancellation.
func waitForExit(ctx context.Context, exited func() bool) error {
	ticker := time.NewTicker(processPollDelay)
	defer ticker.Stop()

	for {
		if exited() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for plugin exit: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
