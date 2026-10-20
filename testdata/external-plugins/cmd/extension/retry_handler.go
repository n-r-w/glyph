package main

import (
	"errors"
	"os"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// retryCancelMode selects an explicit retry cancellation.
	retryCancelMode = "cancel"
	// retryFailureMode selects a failed retry handler.
	retryFailureMode = "fail"
	// retryInvokedSignal identifies an invoked public retry handler.
	retryInvokedSignal = "retry-handler-invoked"
)

// runRetryHandler returns the configured public retry-handler action or failure.
func (operation *handleOperation) runRetryHandler() (*extensionv1.HandleResponse, error) {
	signal(operation.signals, retryInvokedSignal)
	action := new(extensionv1.RetryHandlerAction)
	switch os.Getenv(retryHandlerEnvironment) {
	case retryCancelMode:
		action.SetCancel(new(extensionv1.CancelRetry))
	case retryFailureMode:
		return nil, extensionsdk.Fail(internalFailureCode, errors.New("retry handler full unique failure text"))
	default:
		action.SetPreserve(new(extensionv1.PreserveRetry))
	}
	response := new(extensionv1.HandleResponse)
	response.SetRetry(action)
	return response, nil
}
