//go:build !integration

// Package extensionv1 supplies the public Extension SDK.
package extensionv1

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// hostContractMatrixSuite owns the current request-kind contract matrix.
type hostContractMatrixSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestHostRequestContracts checks every current request and completion discriminator.
func TestHostRequestContracts(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &hostContractMatrixSuite{Suite: suite.Suite{}})
}

// TestScenario characterizes current matching, malformed completion, and common failure behavior.
func (s *hostContractMatrixSuite) TestScenario() {
	t := s.T()
	// Arrange the current public request/completion pairs.
	for _, scenario := range []struct {
		// kind identifies the expected initiated contract.
		kind hostRequestKind
		// request selects one public request payload.
		request *extensionpb.ExtensionRequest
		// completed selects its public completion payload.
		completed *extensionpb.HostCompleted
		// validationFailure reports an incomplete required result.
		validationFailure bool
	}{
		{kind: hostRequestModels, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetGetModels(new(extensionpb.GetModelsRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetGetModels(new(extensionpb.GetModelsResult))
			return value
		}(), validationFailure: false},
		{kind: hostRequestProviders, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetGetProviders(new(extensionpb.GetProvidersRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetGetProviders(new(extensionpb.GetProvidersResult))
			return value
		}(), validationFailure: false},
		{kind: hostRequestConfiguredModel, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetConfiguredModel(new(extensionpb.ConfiguredModelRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetConfiguredModel(new(extensionpb.ConfiguredModelResult))
			return value
		}(), validationFailure: false},
		{kind: hostRequestAppendExtension, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetAppendExtension(new(extensionpb.AppendExtensionRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetAppendExtension(new(extensionpb.AppendExtensionResult))
			return value
		}(), validationFailure: false},
		{kind: hostRequestSessionState, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetGetSessionState(new(extensionpb.GetSessionStateRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetGetSessionState(new(extensionpb.GetSessionStateResult))
			return value
		}(), validationFailure: false},
		{kind: hostRequestAppendExtensionMessage, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetAppendExtensionMessage(new(extensionpb.AppendExtensionMessageRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetAppendExtensionMessage(new(extensionpb.AppendExtensionMessageResult))
			return value
		}(), validationFailure: false},
		{kind: hostRequestModelSelection, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetSelectModel(new(extensionpb.SelectModelRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetSelection(new(extensionpb.SelectionResult))
			return value
		}(), validationFailure: true},
		{kind: hostRequestReasoningSelection, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetSelectReasoning(new(extensionpb.SelectReasoningRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetSelection(new(extensionpb.SelectionResult))
			return value
		}(), validationFailure: true},
		{kind: hostRequestCompaction, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetCompact(new(extensionpb.CompactRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetCompact(new(extensionpb.CompactResult))
			return value
		}(), validationFailure: true},
		{kind: hostRequestCompactionProjection, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetProjectCompaction(new(extensionpb.CompactionCandidateRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetProjectCompaction(new(extensionpb.CompactionProjection))
			return value
		}(), validationFailure: false},
		{kind: hostRequestCompactionReplacement, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetReplaceCompaction(new(extensionpb.CompactionCandidateRequest))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetReplaceCompaction(new(extensionpb.CompactResult))
			return value
		}(), validationFailure: true},
		{kind: hostRequestCancel, request: func() *extensionpb.ExtensionRequest {
			value := new(extensionpb.ExtensionRequest)
			value.SetCancel(new(operationpb.CancelOperation))
			return value
		}(), completed: func() *extensionpb.HostCompleted {
			value := new(extensionpb.HostCompleted)
			value.SetCancel(new(operationpb.CancelCompleted))
			return value
		}(), validationFailure: true},
	} {
		// Act through SDK request classification and terminal validation.
		require.Equal(t, scenario.kind, classifyHostRequest(scenario.request))
		require.True(t, hostCompletedMatches(scenario.kind, scenario.completed))
		require.False(t, hostCompletedMatches(scenario.kind, nil))
		err := validateHostCompleted(scenario.kind, scenario.completed)
		// Assert exact established malformed-result behavior and common code allowances.
		if scenario.validationFailure {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, validateHostFailureCode(scenario.kind, failureCodeInternal))
		require.NoError(t, validateHostFailureCode(scenario.kind, contextCodeStale))
		require.Error(t, validateHostFailureCode(scenario.kind, "UNKNOWN_CATEGORY"))
	}
	require.Equal(t, hostRequestInvalid, classifyHostRequest(nil))
	require.Equal(t, hostRequestInvalid, classifyHostRequest(new(extensionpb.ExtensionRequest)))
	require.False(t, hostCompletedMatches(hostRequestInvalid, new(extensionpb.HostCompleted)))
	require.NoError(t, validateHostFailureCode(hostRequestInvalid, failureCodeInternal))
	require.Error(t, validateHostCompleted(hostRequestInvalid, new(extensionpb.HostCompleted)))
}

// TestFailureFamilies checks accepted and unrelated categories for each shared contract family.
func (s *hostContractMatrixSuite) TestFailureFamilies() {
	t := s.T()
	// Arrange the independent append, selection, and compaction failure contracts.
	families := []struct {
		// kinds identifies the request variants with this failure contract.
		kinds []hostRequestKind
		// allowed lists the exact operation-specific categories for those variants.
		allowed []string
	}{
		{
			kinds:   []hostRequestKind{hostRequestAppendExtension, hostRequestAppendExtensionMessage},
			allowed: []string{hostFailureCodePersistenceUnavailable, hostFailureCodeSessionUnavailable},
		},
		{
			kinds: []hostRequestKind{hostRequestModelSelection, hostRequestReasoningSelection},
			allowed: []string{
				hostFailureCodeModelUnavailable, hostFailureCodeCredentialUnavailable,
				hostFailureCodeExtensionRejected, hostFailureCodeExtensionUnavailable,
			},
		},
		{
			kinds: []hostRequestKind{
				hostRequestCompaction,
				hostRequestCompactionProjection,
				hostRequestCompactionReplacement,
			},
			allowed: []string{
				hostFailureCodeCompactionFailed, hostFailureCodeExtensionFailed,
				hostFailureCodeSessionUnavailable, hostFailureCodePersistenceUnavailable,
			},
		},
	}
	// categories includes related categories and categories that belong to other requests.
	categories := []string{
		hostFailureCodeModelUnavailable, hostFailureCodeCredentialUnavailable,
		hostFailureCodeModelFailed, hostFailureCodeRetryExhausted, hostFailureCodeRetryCanceled,
		hostFailureCodeExtensionFailed, hostFailureCodeRetryDelayExceeded, hostFailureCodeContextLimit,
		hostFailureCodeSessionUnavailable, hostFailureCodePersistenceUnavailable,
		hostFailureCodeExtensionRejected, hostFailureCodeExtensionUnavailable, hostFailureCodeCompactionFailed,
	}
	for _, family := range families {
		for _, kind := range family.kinds {
			for _, category := range categories {
				// Act through SDK category validation for this request.
				err := validateHostFailureCode(kind, category)
				// Assert the independent category set, including rejection of unrelated categories.
				if slices.Contains(family.allowed, category) {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		}
	}
}
