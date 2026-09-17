//go:build integration

package extensionv1

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

const (
	// selectionRejectedFailureCode is the public explicit-handler rejection category.
	selectionRejectedFailureCode = "EXTENSION_REJECTED"
	// selectionUnavailableFailureCode is the public selected-runtime failure category.
	selectionUnavailableFailureCode = "EXTENSION_UNAVAILABLE"
	// modelSelectionFailureText is the complete model-selection rejection text.
	modelSelectionFailureText = "complete model selection handler rejection"
	// reasoningSelectionFailureText is the complete reasoning-selection runtime failure text.
	reasoningSelectionFailureText = "complete reasoning selection runtime failure"
)

// TestSelectionFailuresRemainRequestLocal verifies both selection failure kinds and continued real-stream use.
func TestSelectionFailuresRemainRequestLocal(t *testing.T) {
	t.Parallel()

	// Arrange: return one documented failure for each selection kind with a successful catalog read after each.
	controller := gomock.NewController(t)
	service := NewMockService(controller)
	registration := NewMockRegisterOperation(controller)
	execution := NewMockExecuteOperation(controller)
	host := NewMockHostService(controller)
	modelSelection := NewMockHostOperation(controller)
	firstCatalog := NewMockHostOperation(controller)
	reasoningSelection := NewMockHostOperation(controller)
	secondCatalog := NewMockHostOperation(controller)
	service.EXPECT().PrepareRegister(gomock.Any(), gomock.Any()).Return(registration, nil)
	registration.EXPECT().Run(gomock.Any()).Return(contractRegistration(), nil)
	registration.EXPECT().Release()
	service.EXPECT().PrepareExecute(gomock.Any(), gomock.Any()).Return(execution, nil)
	execution.EXPECT().Release()
	gomock.InOrder(
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
				require.NotNil(t, request.GetSelectModel())
				return modelSelection, nil
			},
		),
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
				require.NotNil(t, request.GetGetModels())
				return firstCatalog, nil
			},
		),
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
				require.NotNil(t, request.GetSelectReasoning())
				return reasoningSelection, nil
			},
		),
		host.EXPECT().Prepare(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ string, request *extensionpb.ExtensionRequest) (HostOperation, error) {
				require.NotNil(t, request.GetGetModels())
				return secondCatalog, nil
			},
		),
	)
	modelSelection.EXPECT().Run(gomock.Any()).Return(
		nil,
		Fail(selectionRejectedFailureCode, errors.New(modelSelectionFailureText)),
	)
	modelSelection.EXPECT().Release()
	firstCatalog.EXPECT().Run(gomock.Any()).Return(emptyModelCatalogue(), nil)
	firstCatalog.EXPECT().Release()
	reasoningSelection.EXPECT().Run(gomock.Any()).Return(
		nil,
		Fail(selectionUnavailableFailureCode, errors.New(reasoningSelectionFailureText)),
	)
	reasoningSelection.EXPECT().Release()
	secondCatalog.EXPECT().Run(gomock.Any()).Return(emptyModelCatalogue(), nil)
	secondCatalog.EXPECT().Release()
	failedTerminals := 0
	execution.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ *ProgressReporter) (*extensionpb.ToolResult, error) {
			binding, err := ContextFrom(ctx)
			if err != nil {
				return nil, err
			}
			modelOperation, err := binding.StartModelSelection(ctx, extensionpb.SelectModelRequest_builder{
				Context: nil, ProviderId: new("provider"), ModelId: new("model"),
			}.Build())
			if err != nil {
				return nil, err
			}
			_, err = modelOperation.Wait(ctx)
			if err = assertRequestLocalSelectionFailure(
				t, err, selectionRejectedFailureCode, modelSelectionFailureText,
			); err != nil {
				return nil, err
			}
			failedTerminals++
			if err = assertCatalogStillAvailable(ctx, binding); err != nil {
				return nil, err
			}
			reasoningOperation, err := binding.StartReasoningSelection(ctx, extensionpb.SelectReasoningRequest_builder{
				Context: nil, ReasoningChoice: new("high"),
			}.Build())
			if err != nil {
				return nil, err
			}
			_, err = reasoningOperation.Wait(ctx)
			if err = assertRequestLocalSelectionFailure(
				t, err, selectionUnavailableFailureCode, reasoningSelectionFailureText,
			); err != nil {
				return nil, err
			}
			failedTerminals++
			if err = assertCatalogStillAvailable(ctx, binding); err != nil {
				return nil, err
			}
			return extensionpb.ToolResult_builder{Contents: nil, IsError: new(false)}.Build(), nil
		},
	)
	connection := openContextTestConnection(t, service, host)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	registrationRequest := new(extensionpb.HostRequest)
	registrationRequest.SetRegister(new(extensionpb.RegisterRequest))
	registered, err := connection.Start(ctx, "register", registrationRequest)
	require.NoError(t, err)
	_, err = registered.Wait(ctx, nil)
	require.NoError(t, err)
	request := new(extensionpb.HostRequest)
	request.SetExecute(extensionpb.ExecuteRequest_builder{
		ToolName: new("contract"), ArgumentsJson: []byte(`{}`), Context: testInvocationIdentity(),
	}.Build())

	// Act: run both failing selection requests and both later catalog reads on one real SDK stream.
	invoked, err := connection.Start(ctx, "selection-failures", request)
	require.NoError(t, err)
	_, err = invoked.Wait(ctx, nil)

	// Assert: each selection produced one request-local failed terminal and the stream remained usable.
	require.NoError(t, err)
	assert.Equal(t, 2, failedTerminals)
}

// assertRequestLocalSelectionFailure waits for one failed terminal and checks its exact public contract.
func assertRequestLocalSelectionFailure(t *testing.T, err error, code string, message string) error {
	t.Helper()
	if err == nil {
		return errors.New("selection operation unexpectedly completed")
	}
	var failure *FailureError
	if !errors.As(err, &failure) {
		return err
	}
	if failure.Code() != code {
		return err
	}
	if !assert.Contains(t, err.Error(), message) {
		return err
	}
	return nil
}

// assertCatalogStillAvailable proves the same stream accepts and completes later work.
func assertCatalogStillAvailable(ctx context.Context, binding *ExtensionContext) error {
	catalog, err := binding.StartGetModels(ctx)
	if err != nil {
		return err
	}
	_, err = catalog.Wait(ctx)
	return err
}
