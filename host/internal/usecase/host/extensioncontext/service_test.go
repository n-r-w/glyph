//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestBindingsNeverReactivate verifies runtime replacement, same-ID resume, and A-to-B-to-A invalidation.
func TestBindingsNeverReactivate(t *testing.T) {
	t.Parallel()

	// Arrange: isolate runtime and session identity behind generated state-owner mocks.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	session := NewMockSessionState(controller)
	instance := "runtime-1"
	identity := SessionIdentity{ID: "A", WorkingDirectory: "/project", Incarnation: 1}
	runtime.EXPECT().
		ContextRuntime("extension").
		DoAndReturn(func(string) (string, bool) { return instance, true }).
		AnyTimes()
	session.EXPECT().ContextSession().DoAndReturn(func() SessionIdentity { return identity }).AnyTimes()
	service := New(runtime, session)

	// Act: issue and reuse a binding before each permanent replacement.
	first, err := service.IssueContext("extension")
	require.NoError(t, err)
	repeated, err := service.IssueContext("extension")
	require.NoError(t, err)
	require.Equal(t, first, repeated)
	old := extension.ContextRef{ID: first.ID, RuntimeInstanceID: first.RuntimeInstanceID, SessionID: first.SessionID}
	require.NoError(t, service.ValidateContext("extension", instance, old))
	for _, replacement := range []SessionIdentity{
		{ID: "A", WorkingDirectory: "/project", Incarnation: 2},
		{ID: "B", WorkingDirectory: "/project", Incarnation: 3},
		{ID: "A", WorkingDirectory: "/project", Incarnation: 4},
	} {
		identity = replacement
		current, issueErr := service.IssueContext("extension")
		require.NoError(t, issueErr)
		assert.NotEqual(t, first.ID, current.ID)
		assertStaleContext(t, service.ValidateContext("extension", instance, old))
		currentRef := extension.ContextRef{ID: current.ID, RuntimeInstanceID: instance, SessionID: current.SessionID}
		require.NoError(t, service.ValidateContext("extension", instance, currentRef))
	}
	instance = "runtime-2"
	current, err := service.IssueContext("extension")
	require.NoError(t, err)

	// Assert: public identity is exact and neither session nor runtime replacement restores an old binding.
	assert.Equal(t, "extension", current.ExtensionID)
	assert.Equal(t, instance, current.RuntimeInstanceID)
	assert.Equal(t, "A", current.SessionID)
	assert.Equal(t, "/project", current.WorkingDirectory)
	assert.NotEmpty(t, current.ID)
	assertStaleContext(t, service.ValidateContext("extension", "runtime-1", old))
	mismatched := extension.ContextRef{ID: current.ID, RuntimeInstanceID: instance, SessionID: "B"}
	assertStaleContext(t, service.ValidateContext("extension", instance, mismatched))
}

// TestCataloguesRevalidateBlockedReads verifies stale completion and ordered provider-neutral projection.
func TestCataloguesRevalidateBlockedReads(t *testing.T) {
	t.Parallel()
	for _, replaced := range []string{"session", "runtime"} {
		t.Run(replaced, func(t *testing.T) {
			t.Parallel()

			// Arrange: block catalog access while the active-session incarnation changes.
			controller := gomock.NewController(t)
			runtime := NewMockRuntimeState(controller)
			session := NewMockSessionState(controller)
			catalog := NewMockCatalog(controller)
			var mutex sync.Mutex
			identity := SessionIdentity{ID: "A", WorkingDirectory: "/project", Incarnation: 1}
			instance := "runtime"
			runtime.EXPECT().ContextRuntime("extension").DoAndReturn(func(string) (string, bool) {
				mutex.Lock()
				defer mutex.Unlock()
				return instance, true
			}).AnyTimes()
			session.EXPECT().ContextSession().DoAndReturn(func() SessionIdentity {
				mutex.Lock()
				defer mutex.Unlock()
				return identity
			}).AnyTimes()
			service := New(runtime, session)
			service.BindCatalog(catalog)
			binding, err := service.IssueContext("extension")
			require.NoError(t, err)
			reference := extension.ContextRef{ID: binding.ID, RuntimeInstanceID: "runtime", SessionID: "A"}
			entered := make(chan struct{})
			release := make(chan struct{})
			catalog.EXPECT().Models().DoAndReturn(func() []model.Descriptor {
				close(entered)
				<-release
				return nil
			})
			catalog.EXPECT().ActiveSelection().Return(model.Selection{})
			result := make(chan error, 1)

			// Act: replace the session after admission but before the catalog read finishes.
			go func() {
				_, readErr := service.ReadModels(t.Context(), "extension", "runtime", reference)
				result <- readErr
			}()
			<-entered
			mutex.Lock()
			if replaced == "session" {
				identity.Incarnation++
			} else {
				instance = "replacement-runtime"
			}
			mutex.Unlock()
			close(release)

			// Assert: the old result cannot complete successfully after replacement.
			assertStaleContext(t, <-result)
			fresh, err := service.IssueContext("extension")
			require.NoError(t, err)
			freshRef := extension.ContextRef{ID: fresh.ID, RuntimeInstanceID: fresh.RuntimeInstanceID, SessionID: "A"}
			catalog.EXPECT().Models().Return([]model.Descriptor{
				catalogueDescriptor("provider-a", "second"),
				catalogueDescriptor("provider-a", "first"),
				catalogueDescriptor("provider-b", "other"),
			})
			providers, err := service.ReadProviders(t.Context(), "extension", fresh.RuntimeInstanceID, freshRef)
			require.NoError(t, err)
			assert.Equal(t, []extensioncontroller.Provider{
				{ID: "provider-a", ModelIDs: []model.ID{"second", "first"}},
				{ID: "provider-b", ModelIDs: []model.ID{"other"}},
			}, providers)
			canceled, cancel := context.WithCancel(t.Context())
			cancel()
			_, err = service.ReadProviders(canceled, "extension", fresh.RuntimeInstanceID, freshRef)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

// TestHiddenAppendAndRecoveryUseIssuedIncarnation verifies session work uses one bound identity.
func TestHiddenAppendAndRecoveryUseIssuedIncarnation(t *testing.T) {
	t.Parallel()

	// Arrange one valid binding and state-owner results with exact opaque bytes.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	identity := SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 3}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	runtime.EXPECT().BeginContextCommit("extension", "runtime").Return(func() {}, nil)
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	service := New(runtime, sessions)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
	payload := []byte(`{ "escaped": "\u0061" }`)
	stored := session.Entry{
		ID:            "entry",
		ParentID:      mo.Some("foreign-parent"),
		CreatedAt:     time.Unix(9, 0).UTC(),
		Information:   mo.None[session.Information](),
		User:          mo.None[session.UserMessage](),
		Model:         mo.None[session.ModelResponse](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension: mo.Some(
			session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "checkpoint", Data: payload},
		),
		BranchSummary: mo.None[session.BranchSummaryEntry](), ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
	sessions.EXPECT().AppendExtension(
		gomock.Any(), identity, stored.Extension.MustGet(), gomock.Any(),
	).DoAndReturn(func(
		_ context.Context,
		_ SessionIdentity,
		_ session.ExtensionEnvelope,
		guard ContextCommitGuard,
	) (session.Entry, error) {
		release, guardErr := guard()
		if guardErr != nil {
			return session.Entry{}, guardErr
		}
		defer release()
		return stored, nil
	})
	sessions.EXPECT().ExtensionState(gomock.Any(), identity, "extension").Return(SessionSnapshot{
		SessionID: "session", ActiveLeafID: mo.Some("entry"), Entries: []session.Entry{stored},
	}, nil)

	// Act through both context-owned session capabilities.
	appended, err := service.AppendExtension(t.Context(), "extension", "runtime", reference, "checkpoint", payload)
	require.NoError(t, err)
	snapshot, err := service.ReadSessionState(t.Context(), "extension", "runtime", reference)

	// Assert exact entry metadata and bytes pass through without interpretation.
	require.NoError(t, err)
	assert.Equal(t, stored, appended)
	assert.Equal(t, payload, snapshot.Entries[0].Extension.MustGet().Data)
	assert.Equal(t, mo.Some("foreign-parent"), snapshot.Entries[0].ParentID)
}

// TestMessageAppendReturnsCommittedEntryWithDeliveryIssue verifies post-commit publication failure is nonterminal.
func TestMessageAppendReturnsCommittedEntryWithDeliveryIssue(t *testing.T) {
	t.Parallel()

	// Arrange one valid binding and a session owner that commits before its client publisher fails.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	identity := SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 3}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	runtime.EXPECT().BeginContextCommit("extension", "runtime").Return(func() {}, nil)
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	service := New(runtime, sessions)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
	message := session.ExtensionMessage{
		ExtensionID: "extension", EntryType: "note", Text: "exact text", Visibility: session.ClientVisibilityHidden,
	}
	stored := session.Entry{
		ID: "message", ParentID: mo.Some("parent"), CreatedAt: time.Unix(10, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.Some(message), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	deliveryErr := errors.New("ordered writer failed")
	service.BindMessagePublisher(func(entry session.Entry) (func(context.Context) error, error) {
		assert.Equal(t, stored, entry)
		return func(context.Context) error { return deliveryErr }, nil
	})
	sessions.EXPECT().AppendExtensionMessage(
		gomock.Any(), identity, message, gomock.Any(), gomock.Any(),
	).DoAndReturn(func(
		_ context.Context,
		_ SessionIdentity,
		_ session.ExtensionMessage,
		guard ContextCommitGuard,
		publisher func(session.Entry) (func(context.Context) error, error),
	) (session.Entry, error) {
		release, guardErr := guard()
		require.NoError(t, guardErr)
		defer release()
		wait, publishErr := publisher(stored)
		require.NoError(t, publishErr)
		return stored, wait(t.Context())
	})

	// Act through the context-owned message append capability.
	result, err := service.AppendExtensionMessage(
		t.Context(), "extension", "runtime", reference, "note", "exact text", session.ClientVisibilityHidden,
	)

	// Assert the committed entry remains usable and the complete delivery cause is one nonterminal issue.
	require.NoError(t, err)
	assert.Equal(t, stored, result.Entry)
	require.Len(t, result.Issues, 1)
	assert.Equal(t, "extension", result.Issues[0].ExtensionID)
	assert.Equal(t, "DELIVERY_FAILED", result.Issues[0].Code)
	assert.Contains(t, result.Issues[0].Message, deliveryErr.Error())
}

// TestMessageAppendWithoutPublisherReportsCommittedDeliveryFailure verifies missing client binding is not hidden.
func TestMessageAppendWithoutPublisherReportsCommittedDeliveryFailure(t *testing.T) {
	t.Parallel()

	// Arrange one valid binding and a session owner that reports the missing publisher after commit.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	identity := SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 3}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	service := New(runtime, sessions)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
	message := session.ExtensionMessage{
		ExtensionID: "extension", EntryType: "note", Text: "exact text", Visibility: session.ClientVisibilityVisible,
	}
	stored := session.Entry{
		ID: "message", ParentID: mo.Some("parent"), CreatedAt: time.Unix(10, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.Some(message), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	deliveryErr := errors.New("extension message publisher is not bound")
	sessions.EXPECT().AppendExtensionMessage(
		gomock.Any(), identity, message, gomock.Any(), gomock.Nil(),
	).Return(stored, deliveryErr)

	// Act without binding a Glyph client publisher.
	result, err := service.AppendExtensionMessage(
		t.Context(), "extension", "runtime", reference, "note", "exact text", session.ClientVisibilityVisible,
	)

	// Assert the committed entry and explicit delivery failure remain available to the extension.
	require.NoError(t, err)
	assert.Equal(t, stored, result.Entry)
	require.Len(t, result.Issues, 1)
	assert.Equal(t, deliveryFailedIssueCode, result.Issues[0].Code)
	assert.Contains(t, result.Issues[0].Message, deliveryErr.Error())
}

// TestMessageAppendReportsPostCommitCancellation verifies cancellation cannot hide an already committed entry.
func TestMessageAppendReportsPostCommitCancellation(t *testing.T) {
	t.Parallel()

	// Arrange one valid binding whose session owner reports a committed entry with cancellation.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	identity := SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 3}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	service := New(runtime, sessions)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
	message := session.ExtensionMessage{
		ExtensionID: "extension", EntryType: "note", Text: "exact text", Visibility: session.ClientVisibilityVisible,
	}
	stored := session.Entry{
		ID: "message", ParentID: mo.Some("parent"), CreatedAt: time.Unix(10, 0).UTC(),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), EstimatedCost: mo.None[session.EstimatedCost](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		ExtensionMessage: mo.Some(message), BranchSummary: mo.None[session.BranchSummaryEntry](),
	}
	sessions.EXPECT().AppendExtensionMessage(
		gomock.Any(), identity, message, gomock.Any(), gomock.Any(),
	).Return(stored, context.Canceled)

	// Act after the state owner has already committed the message.
	result, err := service.AppendExtensionMessage(
		t.Context(), "extension", "runtime", reference, "note", "exact text", session.ClientVisibilityVisible,
	)

	// Assert cancellation is a nonterminal delivery issue and the committed identity remains usable.
	require.NoError(t, err)
	assert.Equal(t, stored, result.Entry)
	require.Len(t, result.Issues, 1)
	assert.Equal(t, "DELIVERY_FAILED", result.Issues[0].Code)
	assert.Contains(t, result.Issues[0].Message, context.Canceled.Error())
}

// TestSessionRecoveryRejectsReplacementDuringRead verifies a stale snapshot cannot complete.
func TestSessionRecoveryRejectsReplacementDuringRead(t *testing.T) {
	t.Parallel()

	// Arrange a valid binding and block its state-owner read before final validation.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	sessions := NewMockSessionState(controller)
	var mutex sync.Mutex
	identity := SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	sessions.EXPECT().ContextSession().DoAndReturn(func() SessionIdentity {
		mutex.Lock()
		defer mutex.Unlock()
		return identity
	}).AnyTimes()
	service := New(runtime, sessions)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
	entered := make(chan struct{})
	release := make(chan struct{})
	sessions.EXPECT().ExtensionState(gomock.Any(), identity, "extension").DoAndReturn(
		func(context.Context, SessionIdentity, string) (SessionSnapshot, error) {
			close(entered)
			<-release
			return SessionSnapshot{
				SessionID:    "session",
				ActiveLeafID: mo.None[string](),
				Entries:      nil,
			}, nil
		},
	)
	result := make(chan error, 1)

	// Act by replacing the active-session incarnation while recovery executes.
	go func() {
		_, readErr := service.ReadSessionState(t.Context(), "extension", "runtime", reference)
		result <- readErr
	}()
	<-entered
	mutex.Lock()
	identity.Incarnation++
	mutex.Unlock()
	close(release)

	// Assert the accepted operation cannot publish the stale snapshot.
	assertStaleContext(t, <-result)
}

// TestConfiguredRequestPassesExactInput verifies context ownership forwards one explicit request unchanged.
func TestConfiguredRequestPassesExactInput(t *testing.T) {
	t.Parallel()

	// Arrange a valid binding, an empty instruction string, and ordered text history.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	session := NewMockSessionState(controller)
	catalog := NewMockCatalog(controller)
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	session.EXPECT().ContextSession().Return(
		SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
	).AnyTimes()
	service := New(runtime, session)
	service.BindCatalog(catalog)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("question")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	expected := model.Response{
		Content: []model.Content{{
			Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	catalog.EXPECT().Request(gomock.Any(), selection, "", history).Return(expected, nil)

	// Act through the session-bound context owner.
	actual, err := service.Request(t.Context(), "extension", "runtime", reference, selection, "", history)

	// Assert the provider response and request values are unchanged.
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

// TestConfiguredRequestRejectsStaleCompletion verifies a replaced binding cannot publish a provider result.
func TestConfiguredRequestRejectsStaleCompletion(t *testing.T) {
	t.Parallel()

	// Arrange a valid binding and block provider execution before changing the session incarnation.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	session := NewMockSessionState(controller)
	catalog := NewMockCatalog(controller)
	var mutex sync.Mutex
	identity := SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	session.EXPECT().ContextSession().DoAndReturn(func() SessionIdentity {
		mutex.Lock()
		defer mutex.Unlock()
		return identity
	}).AnyTimes()
	service := New(runtime, session)
	service.BindCatalog(catalog)
	issued, err := service.IssueContext("extension")
	require.NoError(t, err)
	reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
	entered := make(chan struct{})
	release := make(chan struct{})
	catalog.EXPECT().Request(gomock.Any(), gomock.Any(), "instructions", gomock.Any()).DoAndReturn(
		func(context.Context, model.Selection, string, []agent.HistoryEntry) (model.Response, error) {
			close(entered)
			<-release
			return model.Response{}, nil
		},
	)
	result := make(chan error, 1)

	// Act by replacing the binding while the provider request is running.
	go func() {
		_, requestErr := service.Request(
			t.Context(), "extension", "runtime", reference, model.Selection{}, "instructions",
			[]agent.HistoryEntry{{
				Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("question")),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			}},
		)
		result <- requestErr
	}()
	<-entered
	mutex.Lock()
	identity.Incarnation++
	mutex.Unlock()
	close(release)

	// Assert the stale operation cannot return a usable result.
	assertStaleContext(t, <-result)
}

// TestConfiguredRequestClassifiesProviderFailures verifies every provider-owned failure keeps its complete cause.
func TestConfiguredRequestClassifiesProviderFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		// name identifies the provider failure.
		name string
		// selectionCode contains a provider catalogue category when present.
		selectionCode string
		// expected contains the public configured-request category.
		expected string
	}{
		{name: "missing model", selectionCode: selectionCodeNotFound, expected: modelUnavailableCode},
		{
			name: "unsupported reasoning", selectionCode: selectionCodeReasoningUnsupported,
			expected: modelUnavailableCode,
		},
		{
			name: "credentials", selectionCode: selectionCodeCredentialUnavailable,
			expected: credentialUnavailableCode,
		},
		{name: "provider execution", selectionCode: "", expected: modelFailedCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a valid binding and one provider failure with a complete diagnostic cause.
			controller := gomock.NewController(t)
			runtime := NewMockRuntimeState(controller)
			session := NewMockSessionState(controller)
			catalog := NewMockCatalog(controller)
			runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
			session.EXPECT().ContextSession().Return(
				SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
			).AnyTimes()
			service := New(runtime, session)
			service.BindCatalog(catalog)
			issued, err := service.IssueContext("extension")
			require.NoError(t, err)
			reference := extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"}
			requestErr := errors.New("complete provider cause")
			if test.selectionCode != "" {
				classified := NewMockRequestFailure(controller)
				classified.EXPECT().SelectionCode().Return(test.selectionCode)
				classified.EXPECT().Error().Return(requestErr.Error()).AnyTimes()
				requestErr = classified
			}
			catalog.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(model.Response{}, requestErr)

			// Act through context-owned failure classification.
			_, err = service.Request(
				t.Context(), "extension", "runtime", reference, model.Selection{}, "", nil,
			)

			// Assert category and complete provider cause remain available together.
			var failure extensioncontroller.ContextFailure
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, test.expected, failure.ContextCode())
			assert.Contains(t, err.Error(), "complete provider cause")
		})
	}
}

// TestFreshContextOwnersNeverReuseBindingIDs verifies recreated Host ownership cannot reactivate an encoded preceding
// reference.
func TestFreshContextOwnersNeverReuseBindingIDs(t *testing.T) {
	t.Parallel()

	// Arrange: supply the same durable identity to two independently constructed context owners.
	controller := gomock.NewController(t)
	runtime := NewMockRuntimeState(controller)
	session := NewMockSessionState(controller)
	runtime.EXPECT().ContextRuntime("extension").Return("runtime", true).AnyTimes()
	session.EXPECT().
		ContextSession().
		Return(SessionIdentity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}).
		AnyTimes()

	// Act: issue the first binding from each fresh owner.
	first, err := New(runtime, session).IssueContext("extension")
	require.NoError(t, err)
	second, err := New(runtime, session).IssueContext("extension")
	require.NoError(t, err)

	// Assert: binding identity is not a counter that restarts with Host ownership.
	assert.NotEqual(t, first.ID, second.ID)
}

// catalogueDescriptor supplies a complete provider-neutral test descriptor.
func catalogueDescriptor(provider model.ProviderID, id model.ID) model.Descriptor {
	return model.Descriptor{
		Provider:      provider,
		Model:         id,
		Input:         []model.InputModality{model.InputModalityText},
		ContextWindow: 1000,
		MaxTokens:     100,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false,
			Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default:   model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{},
		Pricing:          mo.None[model.Pricing](),
	}
}

// assertStaleContext checks the closed category and nonempty diagnostic cause.
func assertStaleContext(t *testing.T, err error) {
	t.Helper()
	var failure extensioncontroller.ContextFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, staleContextCode, failure.ContextCode())
	assert.NotEmpty(t, err.Error())
}
