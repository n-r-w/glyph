package main

import (
	"context"
	"fmt"
	"io"
)

// toolRegistry maps every available tool to its owning extension runtime.
type toolRegistry struct {
	owners map[string]*extensionRuntime
}

// newToolRegistry creates an empty Host-side tool registry.
func newToolRegistry() *toolRegistry {
	return &toolRegistry{owners: make(map[string]*extensionRuntime)}
}

// add registers one complete extension or removes every runtime in a tool-name collision.
func (r *toolRegistry) add(runtime *extensionRuntime) []*extensionRuntime {
	conflicts := map[*extensionRuntime]struct{}{}
	for _, toolName := range runtime.tools {
		if owner := r.owners[toolName]; owner != nil {
			conflicts[owner] = struct{}{}
			conflicts[runtime] = struct{}{}
		}
	}

	if len(conflicts) == 0 {
		for _, toolName := range runtime.tools {
			r.owners[toolName] = runtime
		}
		return nil
	}

	removed := make([]*extensionRuntime, 0, len(conflicts))
	for conflict := range conflicts {
		r.remove(conflict)
		conflict.close()
		removed = append(removed, conflict)
	}
	return removed
}

// remove immediately removes every tool owned by one unavailable runtime.
func (r *toolRegistry) remove(runtime *extensionRuntime) {
	for toolName, owner := range r.owners {
		if owner == runtime {
			delete(r.owners, toolName)
		}
	}
}

// execute routes one call or returns a model-visible unavailable-tool result.
func (r *toolRegistry) execute(ctx context.Context, toolName, input string) (toolOutcome, error) {
	owner := r.owners[toolName]
	if owner == nil {
		return toolOutcome{
			content: fmt.Sprintf("tool %q is unavailable", toolName),
			isError: true,
		}, nil
	}

	stream, err := owner.client.execute(ctx, toolName, input)
	if err != nil {
		return toolOutcome{}, err
	}
	_, result, err := stream.collectExecution()
	return result, err
}

// collectExecution consumes ordered progress until the single terminal result.
func (stream *executionStream) collectExecution() ([]string, toolOutcome, error) {
	var progress []string
	for {
		event, err := stream.recv()
		if err != nil {
			if err == io.EOF {
				return nil, toolOutcome{}, fmt.Errorf("collect execution: stream ended without result")
			}
			return nil, toolOutcome{}, fmt.Errorf("collect execution: %w", err)
		}
		if event.result != nil {
			return progress, *event.result, nil
		}
		progress = append(progress, event.progress)
	}
}
