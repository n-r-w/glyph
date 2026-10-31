# Technical solution: PHS-08 model-facing middleware

## Problem statement

See [problem.md](problem.md). [Requirements](requirements.md), the [ticket](ticket.md), and [confirmed boundaries](target-picture.md) define the required result.

## Proposed solution

### Data ownership and public operations

Introduce one Host capability owner in `host/internal/usecase/host/middleware`. It owns ordered handler registration, immutable original/current operation values, action validation, final semantic validation and extension-failure meaning. Its operation values are provider-neutral domain data or safe wire data. Its dependencies are consumer-owned interfaces implemented by runtime management, extension-context issuance and the provider catalogue. It imports neither concrete implementations nor protobuf.

Extend the existing Extension Contract with typed middleware invocations and actions. System instructions, request-local history, finalized messages, provider headers and serialized request bodies are transformers with preserve/replace actions. User input is a replaceable operation with preserve/replace/handle actions. Provider response metadata is an observer with acknowledgement only. A successful fully-handled input ends that input chain. Invalid or mismatched actions fail the invoking operation as extension failure.

Keep the existing operation stream and SDK admission model. `api/plugins/extension/v1/middleware.proto` contains the new public representations. Existing registration and handler envelopes reference them. `pkg/plugins/extension/v1` remains generated. SDK validation admits only the payload declared by the registered handler kind. Existing extension-process registration order remains authoritative. Startup passes accepted middleware declarations to their owner after validating the complete registration. Runtime management provides invocation mechanics and exact-generation availability checks without acquiring middleware policy.

Each operation owns detached copies of its original and current values. Mutable image data, history content, JSON bytes and header collections are cloned at ownership transfer. State belongs to an invocation or prepared model execution. Existing registry synchronization protects registration snapshots; there is no process-global prepared-request table or application queue.

This method satisfies FRQ-05 and the public operation portion of FRQ-01 through FRQ-04 and NRQ-01.

### Public and internal user input

Replace the public text-only user-submit payloads with ordered text/image content blocks in Programmatic Control and the UI Plugin Contract. Each public package owns its input value definition rather than importing another plugin contract for reuse. Transport controllers validate block shape and map into `model.Message`. Standard TUI and one-shot text paths construct one text block. This phase adds no image-picker UI or new CLI image flags.

The shared runcontrol owner consumes a provider-neutral input-processing port. Processing runs on the accepted operation worker while its existing run reservation is held, before Core execution. The middleware owner composes input handlers and then finalized-user handlers. Host validates the actual final unhandled message against the current selected model descriptor. Structural handler-result failures are extension failures; well-formed final input unsupported by that model has the closed public category `INPUT_INVALID`. The selected descriptor remains the sole source of modality support. A final context check also validates content against the actual model-request snapshot.

A fully-handled input completes the existing client operation successfully without Core invocation, conversation append, model dispatch, synthetic agent events or Core settlement. Runcontrol releases its reservation. UI preparation restores idle availability through its existing release path. One-shot returns success for handled input. An extension uses existing extension operations for any separate user-visible notification; Host fabricates no assistant reply.

For example, an image removed by a handler becomes valid text input for a text-only model. A retained image produces `INPUT_INVALID` before agent-start. A handled image completes without consulting an unused provider's credentials.

This method satisfies FRQ-02, FRQ-06 and FRQ-07. It preserves the existing asynchronous operation lifecycle.

### Effective-context port and prepared provider binding

Agent Core declares a minimal effective-context port in `host/internal/usecase/agent/run`. After assembling provider-neutral history and tools, Core requests effective context through that port. The returned value contains final provider-neutral request data and a provider execution capability. It contains no handler identities, registration order, runtime state, compaction controls or transport types.

`modelexecution.Service` implements the Core port. It retains ownership of selected-provider resolution, captured conversation policy, existing preparation and model execution. A request-local provider binding retains the resolved raw provider and captured conversation owner. It executes the prepared request without repeating initial preparation or recapturing active selection. Its methods receive context explicitly; the object stores no `context.Context`.

The middleware owner implements a context-composition interface consumed by model execution. System-instruction transformation applies to the request's instructions. Generic context transformation applies to history after the existing conversation preparation. This placement is necessary because `capturedConversation.prepare` currently obtains canonical prepared history from sessions, which would overwrite a transformation applied earlier. Final transformed context is validated against the selected descriptor before dispatch.

Ordinary transient retries receive detached copies of the same effective request. They do not repeat instruction/context handlers. If admitted overflow recovery commits replacement history, that replacement is a new prepared-context revision. Host composes its instruction/context values using the same captured middleware registrations, provider selection and conversation owner. The immutable original for that new invocation is the newly prepared history and the request's untransformed base instructions. A failed context chain stops the execution; Host never invokes it again to recover from that failure.

This distinction preserves arbitrary replacement and existing overflow recovery. Given original history H, a handler can return any value F(H). After compaction returns C(H), Host cannot infer F(C(H)) from F(H). Reusing F(H) would ignore recovery, and replacing it with C(H) would discard successful middleware changes. Recomposition is therefore limited to a successfully prepared new revision. The PRD requires context processing before provider requests and does not promise a single successful invocation across changed history revisions.

Configured-model callers use the same instruction/context composition and final modality validation with their explicit selection and history. They retain their current absence of active-conversation capture and automatic compaction. Thus a compaction generator's configured model request can complete without recursively starting automatic compaction. No blanket middleware bypass is added for nested configured-model requests. A handler that explicitly requests another model retains responsibility for its own finite behavior.

This method satisfies FRQ-01, FRQ-05, FRQ-07 and the Core interface requirements in NRQ-01 while preserving PHS-06 ownership.

### Finalized messages and durable history

The middleware owner composes role-preserving finalized-message replacements separately from request-local context changes. A replacement intentionally becomes canonical message content; a context transformation remains local to model dispatch.

User finalization belongs to the shared input path before modality validation. Core consumes that final user message without a second user-finalization invocation. Core declares a specific provider-neutral finalization port for terminal model responses and completed tool results. It applies the returned value before append and uses that same value for completion events, tool-call execution and subsequent turn behavior. The finalization call stays outside the persistence-only append error wrapper, so its errors retain extension meaning.

Configured-model terminal responses use the same finalized-message owner before their results return to the caller. Ordinary conversation responses are finalized by Core only, avoiding double invocation. A model replacement preserves accounting, provider identity and other Host-owned execution metadata. A tool-result replacement preserves the finalized call's identity. Role changes and inconsistent content are invalid actions. Public message projection excludes opaque provider context and preserves or deliberately discards it through the reference mechanism described below.

A failed finalized-message handler stops later handlers and prevents append of its candidate. Existing committed messages and completed tool actions remain committed. Host does not invent another replacement candidate and reenter the failing chain. Cancellation stops new handler work; existing terminal cancellation bookkeeping remains under its current owner and does not retry a canceled chain.

For example, replacing a tool-result text changes both its committed result and the result shown in completion events. Replacing finalized model content changes the model message used by the next request. Neither behavior silently edits the history behind Core's result.

This method satisfies FRQ-03, FRQ-06 and FRQ-07.

### Provider-neutral wire callbacks

Extend the raw provider-attempt protocol in `modelexecution` with neutral callback values, following its existing semantic stream callback convention. Host supplies request transformation and response observation callbacks. Their data contains no HTTP, SDK, protobuf, credential or provider-specific implementation types.

The shared provider infrastructure maps these callbacks through the SDK's existing `option.WithMiddleware`. Both Chat Completions and Responses support this hook in the declared SDK version. The hook receives the final serialized streaming request before network execution and response metadata before SDK error decoding or SSE consumption. The provider adapters retain serialization, authentication, source classification and streaming ownership. Codex retains its existing error-capture transport for acquired provider diagnostics.

Wire middleware runs for each genuine raw provider attempt, with fresh immutable original/current wire values. Header handlers complete before serialized-body handlers. Successful response observers run in registration order before the SDK consumes the response. A successful observer returns the actual response unchanged. An observer failure stops later observers, closes the response body, retains any close error and stops model execution. Normal HTTP failures continue through the existing provider classifier when observers succeed.

Each attempt retains an acquired middleware failure independently of the SDK error return. This is required because the SDK can return context cancellation after its middleware has already acquired another error. Host checks that retained failure and neutral transformation-validation errors before provider retry classification. It exposes `EXTENSION_FAILED`, retains complete non-secret causes and schedules no retry handler, overflow recovery or further raw attempt to recover from middleware failure.

For example, a successful observer of HTTP 429 permits the normal retry policy. An observer failure on that same 429 produces one provider request, no later observer and no provider retry.

This method satisfies FRQ-04, FRQ-06, FRQ-07 and provider independence in NRQ-01.

### Confidential projection and restoration

Ordinary extension operations never receive resolved credentials or opaque provider reasoning bytes. Typed history/message mapping and provider wire mapping each maintain invocation-local references to private data. A reference contains no provider reasoning identifier, encrypted content or secret value.

For typed model history and finalized messages, transport mapping exposes visible content plus local references for private replay owned by that content. Runtime mapping restores recognized references from the invocation's private table before returning the domain action to its capability owner. Preserving a value preserves its private data without reserializing it. Replacing a value from its immutable original can restore references removed by an earlier handler.

At the provider wire boundary, the shared OpenAI infrastructure recognizes actual provider-owned replay locations already produced by the adapters. Responses reasoning input items and OpenRouter Chat replay details become local references in the safe JSON representation. The owning adapter retains their exact original JSON privately. A replacement preserving a reference restores that fragment; removing it deliberately omits replay. Unknown, altered, duplicated or misplaced references fail before dispatch. Header-only transformations preserve original body bytes unchanged. Host middleware coordination knows no OpenAI replay schema.

Header projection excludes authentication, cookies, proxy authentication, account identity and provider-owned secret header values. Extensions can replace public headers and add ordinary custom headers. Writes to protected credential/identity headers fail. Response observers receive status and non-secret headers; protected cookie/authentication headers are excluded and known authentication secrets are redacted. Request URLs, HTTP clients and credentials are outside public payloads. Existing complete-error requirements remain subject only to secret redaction; ordinary account, organization and project identifiers remain in error text.

The public contract describes serialized replacement as this safe representation. It does not promise authenticated raw-request access. This resolves the available interpretation in favor of the existing mandatory confidentiality rule, without changing that rule. No generic secret-discovery engine or second complete provider-schema validator is added.

This method satisfies NRQ-02 while preserving FRQ-01, FRQ-03 and FRQ-04.

### Error categories and operation completion

Use `EXTENSION_FAILED` for ordinary middleware errors, runtime loss and invalid actions. Introduce `INPUT_INVALID` for structurally valid final user input rejected by selected-model modalities. Programmatic/UI failure contracts, allowlists, SDK failure surfaces and Host mappings admit these meanings with complete error text. Existing persistence, compaction, retry and provider categories retain their owners. One-shot errors retain complete text and expose the current public category where present through their visible error formatting.

Pure owning cancellation follows the existing canceled-operation path. If Host already acquired a distinct handler failure, cancellation remains a contributing cause and does not replace the extension category. Failure before provider dispatch has zero provider calls. Failure after a separate durable compaction commit retains that commit without persisting request-local markers.

This method satisfies FRQ-06 and shared-mode behavior in FRQ-07.

### Package and import boundaries

Core owns its effective-context and specific finalized-message interfaces. Model execution implements effective context and owns the middleware callback protocol it consumes. Runcontrol owns its input-processing interface in domain terms. The middleware implementation imports consumer packages only to implement their contracts and declare assertions. Consumers hold their own interfaces and never import the concrete middleware package.

Runtime management implements the middleware owner's invocation interface. Plugin infrastructure maps that neutral invocation to public protobuf. Startup consumes a middleware registration interface. The middleware owner implements it using startup's accepted declarations. Extension-context issuance remains injected through a consumer interface. Application assemblies wire all concrete implementations.

These directions avoid the existing-cycle hazards: Core already imports runcontrol, model execution already imports Core and extension controller contracts, and extensionruntime already imports model execution. No proposed reverse dependency is added. Every implementing struct retains a compile-time assertion at its declaration. Interface-cycle failures require correction of the offending dependency edge, not suppression or interface relocation.

This method satisfies NRQ-01. New implementation paths belong in the architecture tree when implemented.

### Reference extensions and verification

A separately built reference extension uses public contracts/SDK to demonstrate project instructions, context projection and provider-request changes. Its example prompt content is embedded from an English document. Tests exercise logic and observable transformation effects rather than freezing prompt wording. Documentation explains registration order, preserve/replace/handle/observe semantics, safe replay references and failure categories.

Use existing real Programmatic/UI/extension-process and HTTP-provider fixtures for acceptance. Isolated owner tests use generated mocks. Public probes compare provider requests with persisted session entries, count provider/handler invocations, and verify failure category plus complete source text. Provider substitution tests exercise a different raw-provider implementation through the same neutral callback protocol without changing Core or configured-model callers.

Focused RED cases precede implementation. All mandatory project checks and a build follow completed units, and the final acceptance report maps every ticket criterion to executed evidence or an explicit actual limitation. This satisfies NRQ-03 and DLV-02.

### Dry-run assessment

The following are design traces, not executed results:

| Case | Design result |
| --- | --- |
| A appends A; B replaces from original plus B | Provider receives B without A; durable history has neither marker |
| Initial automatic compaction | Context handlers receive the post-preparation history, so later preparation cannot overwrite their result |
| Transient retry | The prepared binding reuses effective context and frozen provider/owner; wire handlers see the new raw attempt |
| Committed overflow recovery | A new prepared-context revision is composed; the same successful transformation contract applies to the new history |
| Image removed or fully handled | Removed image permits final text validation; handled input completes without Core/provider work |
| Finalized user introduces image | Final modality validation rejects it for a text-only model before Core |
| Model/tool replacement | Core uses the replacement for append, completion and subsequent execution |
| Configured summary generation | Shared transformations apply without active-conversation preparation or recursive automatic compaction |
| Request handler fails with EOF-like cause | The retained middleware failure wins over provider classification; no provider dispatch/retry |
| Response observer fails on 429 | One dispatch, no later observer/retry; acquired causes remain visible |
| Replay reference retained or removed | Adapter restores only retained references, with no private bytes exposed to the extension |
| Cancellation with acquired handler failure | Operation reports the acquired extension failure and cancellation cause; it does not start recovery |

## Overengineering and overspecification considerations

The request-local prepared provider binding is necessary to retain one selected provider and captured conversation owner while moving preparation behind Core's effective-context port. It replaces implicit preparation in the ordinary stream path; it adds no global token store.

Safe replay references are necessary because real serialized requests contain private provider reasoning context and extensions can replace complete values. A plain raw-body callback would violate an existing confidentiality requirement. The same explicit preserve/discard meaning serves typed messages and wire bodies.

The acquired-failure retention is necessary because the installed SDK can replace a middleware error with context cancellation and provider retry classification otherwise sees SDK descendants. It adds no retry policy or generic failure framework.

Shared capability orchestration serves all seven extension points and all existing application modes. The provider SDK's existing middleware hook removes the need for another transport interception implementation. A prepare/execute provider redesign, Host-owned HTTP executor, reusable transformation language, compatibility branch, per-extension recursive-request bypass, new queue and provider-installation work are excluded.

Exact public field spelling, local helper names, schema-validation helpers and test fixture organization remain implementation details. This solution deliberately specifies no signatures, field lists, code or implementation-unit sequence.

## Open questions

None. Public contract shapes and successful-context recomposition after committed overflow are inside confirmed decision authority and recorded in [decisions.md](decisions.md). Mandatory confidentiality, failure behavior and ownership remain unchanged.

## References

- [Ticket](ticket.md), [requirements](requirements.md), [target picture](target-picture.md), [facts](facts.md).
- [Target architecture](../../architecture.md), especially CNS-05, CNS-06, APC-01, APC-11.1 and ENT-08.
- [PRD](../../prd.md), Extension Capabilities and Extension failure rule.
- `host/internal/usecase/agent/run`: current request assembly, terminal model handling and tool result append.
- `host/internal/usecase/host/modelexecution`: shared execution, captured preparation, retry and configured requests.
- `host/internal/infra/providers/openai`: existing driver serialization and HTTP/stream handling.
- Declared OpenAI SDK `option.WithMiddleware` and `RequestConfig.Execute`: request/response interception and cancellation behavior.
