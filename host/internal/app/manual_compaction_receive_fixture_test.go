//go:build integration

// Package app assembles the host application and its public integration fixtures.
package app

import (
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// receiveManualTerminal starts the public terminal receiver for one operation.
func receiveManualTerminal(
	fixture *programmaticFixture,
	operationID string,
) (chan *programmaticv1.HostEvent, chan error) {
	terminal := make(chan *programmaticv1.HostEvent, 1)
	receiveErrors := make(chan error, 1)
	go func() {
		for {
			response, err := fixture.stream.Recv()
			if err != nil {
				receiveErrors <- err
				return
			}
			event := response.GetEvent()
			if response.GetOperationId() == operationID &&
				(event.HasCompleted() || event.HasFailed() || event.HasCanceled()) {
				terminal <- event
				return
			}
		}
	}()
	return terminal, receiveErrors
}

// countManualStoredEntries counts original user content and durable replacement markers.
func countManualStoredEntries(entries []*programmaticv1.SessionEntry) (originals, markers int) {
	for _, entry := range entries {
		if entry.GetCompaction() != nil {
			markers++
		}
		for _, content := range entry.GetUser().GetContent() {
			if content.GetText() == manualOriginalText {
				originals++
			}
		}
	}
	return originals, markers
}
