package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/protobuf/encoding/protojson"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// retryComposeMode selects two ordered replacement handlers.
	retryComposeMode = "compose"
	// retryComposedHandlerID identifies the second replacement handler.
	retryComposedHandlerID = "compose-retry"
	// retryCompositionAttemptLimit is the first handler's replacement total attempt limit.
	retryCompositionAttemptLimit int64 = 4
	// retryCompositionDelayMilliseconds is the delay added by each replacement handler.
	retryCompositionDelayMilliseconds int64 = 40
	// retryInvocationFile is the filename format for recorded public retry invocations.
	retryInvocationFile = "retry-%d-%s.json"
	// retryCancelMode selects an explicit retry cancellation.
	retryCancelMode = "cancel"
	// retryFailureMode selects a failed retry handler.
	retryFailureMode = "fail"
	// retryFailureOnceMode fails the child retry handler and permits later outer recovery.
	retryFailureOnceMode = "fail-once"
	// retryFailureOnceSignal records the first retry-handler failure.
	retryFailureOnceSignal = "retry-failed-once"
	// retryInvalidMode selects a replacement that fails decision validation.
	retryInvalidMode = "invalid"
	// retryInvokedSignal identifies an invoked public retry handler.
	retryInvokedSignal = "retry-handler-invoked"
)

// runRetryHandler returns the configured public retry-handler action or failure.
func (operation *handleOperation) runRetryHandler() (*extensionv1.HandleResponse, error) {
	signal(operation.signals, retryInvokedSignal)
	action := new(extensionv1.RetryHandlerAction)
	switch os.Getenv(retryHandlerEnvironment) {
	case retryComposeMode:
		invocation := operation.request.GetRetry()
		encoded, err := protojson.Marshal(invocation)
		if err != nil {
			return nil, err
		}
		name := fmt.Sprintf(retryInvocationFile, invocation.GetCompletedAttempts(), operation.request.GetHandlerId())
		if writeErr := os.WriteFile(filepath.Join(operation.signals, name), encoded, signalFileMode); writeErr != nil {
			return nil, writeErr
		}
		delay, limit := retryCompositionDelayMilliseconds, retryCompositionAttemptLimit
		if operation.request.GetHandlerId() == retryComposedHandlerID {
			delay = invocation.GetCurrent().GetDelayMilliseconds() + retryCompositionDelayMilliseconds
			limit = invocation.GetCurrent().GetAttemptLimit() - 1
		}
		action.SetReplace(extensionv1.RetryDecision_builder{
			Retryable: new(true), Retry: new(invocation.GetCompletedAttempts() < limit),
			DelayMilliseconds: new(delay), AttemptLimit: new(limit),
		}.Build())
	case retryInvalidMode:
		action.SetReplace(extensionv1.RetryDecision_builder{
			Retryable: new(true), Retry: new(true), DelayMilliseconds: new(int64(-1)),
			AttemptLimit: new(retryCompositionAttemptLimit),
		}.Build())
	case retryCancelMode:
		action.SetCancel(new(extensionv1.CancelRetry))
	case retryFailureOnceMode:
		path := filepath.Join(operation.signals, retryFailureOnceSignal)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			if writeErr := os.WriteFile(path, nil, signalFileMode); writeErr != nil {
				return nil, writeErr
			}
			return nil, extensionsdk.Fail(internalFailureCode, errors.New("retry handler full unique failure text"))
		} else if err != nil {
			return nil, err
		}
		action.SetPreserve(new(extensionv1.PreserveRetry))
	case retryFailureMode:
		return nil, extensionsdk.Fail(internalFailureCode, errors.New("retry handler full unique failure text"))
	default:
		action.SetPreserve(new(extensionv1.PreserveRetry))
	}
	response := new(extensionv1.HandleResponse)
	response.SetRetry(action)
	return response, nil
}
