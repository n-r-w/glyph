package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// ordinaryProgressText reports work performed by the lifecycle progress fixture.
	ordinaryProgressText = "ordinary effect progress"
	// ordinaryFailureEnvironment selects a lifecycle chain that fails once.
	ordinaryFailureEnvironment = "GLYPH_ORDINARY_OBSERVER_FAILURE"
	// ordinaryFirstObserver identifies the failing participant.
	ordinaryFirstObserver = "ordinary-first"
	// ordinaryLaterObserver identifies the second participant in the selected lifecycle chain.
	ordinaryLaterObserver = "ordinary-later"
	// ordinaryFailureSignal retains the first failure across independent requests.
	ordinaryFailureSignal = "ordinary-failed"
	// ordinaryLaterSignal records invocation of the second lifecycle observer.
	ordinaryLaterSignal = "ordinary-later-called"
	// ordinaryFailureText is the complete public observer failure.
	ordinaryFailureText = "ordinary lifecycle observer full unique original cause"
	// ordinaryNestedEnvironment enables a consumed configured-child failure inside the observer.
	ordinaryNestedEnvironment = "GLYPH_ORDINARY_OBSERVER_NESTED"
)

// isOrdinaryFailureObserver identifies fixture-only ordinary lifecycle observers.
func isOrdinaryFailureObserver(id string) bool {
	return id == ordinaryFirstObserver || id == ordinaryLaterObserver
}

// ordinaryFailureObservers supplies ordered observer descriptors for the requested lifecycle event.
func ordinaryFailureObservers() []*extensionv1.HandlerDescriptor {
	kind, ok := extensionv1.HandlerKind_value[os.Getenv(ordinaryFailureEnvironment)]
	if !ok {
		return nil
	}
	return []*extensionv1.HandlerDescriptor{
		extensionv1.HandlerDescriptor_builder{
			Id:   new(ordinaryFirstObserver),
			Kind: new(extensionv1.HandlerKind(kind)),
		}.Build(),
		extensionv1.HandlerDescriptor_builder{
			Id:   new(ordinaryLaterObserver),
			Kind: new(extensionv1.HandlerKind(kind)),
		}.Build(),
	}
}

// runOrdinaryFailureObserver fails once and leaves the same connection available to the next run.
func (operation *handleOperation) runOrdinaryFailureObserver(ctx context.Context) (*extensionv1.HandleResponse, error) {
	response := new(extensionv1.HandleResponse)
	response.SetLifecycle(new(extensionv1.LifecycleAction))
	if operation.request.GetHandlerId() == ordinaryLaterObserver {
		err := os.WriteFile(filepath.Join(operation.signals, ordinaryLaterSignal), nil, signalFileMode)
		return response, err
	}
	failurePath := filepath.Join(operation.signals, ordinaryFailureSignal)
	_, err := os.Stat(failurePath)
	if errors.Is(err, os.ErrNotExist) {
		if writeErr := os.WriteFile(failurePath, []byte(ordinaryFailureText), signalFileMode); writeErr != nil {
			return nil, writeErr
		}
		if os.Getenv(ordinaryNestedEnvironment) != "" {
			_, childErr := requestConsumedConfiguredFailure(ctx, operation.signals)
			return response, childErr
		}
		return nil, errors.New(ordinaryFailureText)
	}
	return response, err
}
