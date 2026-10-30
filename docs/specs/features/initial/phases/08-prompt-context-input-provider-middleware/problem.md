# Problem statement

## Key definitions and abbreviations

Use the [domain glossary](../../../../../terms.md) for extension, original extension input, current extension value, prepared history, input modality, and configured-model request.

## Context

Glyph supports Host-managed extension processes, persistent conversations, model selection, configured-model requests, compaction and retries. Extension authors already compose selection and session-tree operations with ordered handlers. Model-facing customization is the subject of [PHS-08](ticket.md).

## Observed problem

When extension authors need project-specific instructions, request-local context, text/image input processing, finalized-message replacement, or provider-request customization, they cannot express that behavior through the current public extension operations. Users consequently cannot enable those capabilities by loading an ordinary extension.

## Affected audience

- Extension authors who customize the information sent to a model or process user input.
- Users who run those extensions through Programmatic Control, the standard TUI, or one-shot execution.
- Provider-adapter authors whose callers need to customize wire requests or observe provider response metadata.

## Evidence

[Current-behavior facts](facts.md) identify the declarations and traced paths for each case:

- FCT-01 and FCT-02: Public user-submit commands carry text, while the domain and the separate configured-model operation can represent images. The user-submit paths contain no extension transformation or fully-handled result.
- FCT-03: Core constructs requests from its stored instruction string and history projection. Host's existing preparation invokes conversation compaction policy. There is no public generic instruction/context transformation in the inspected handler sets.
- FCT-04: Sessions retains durable entries separately from provider projection. Extension authors currently have no generic request-local context handler through which to customize only one outgoing request.
- FCT-05: Existing message-end callbacks observe a finalized response after its append; they return no replacement message.
- FCT-06: Public registration, SDK admission, and Host dispatch enumerate existing tree, lifecycle, selection, retry and compaction operations, without the model-facing operations above.
- FCT-07: Concrete providers construct SDK/HTTP requests, while the shared raw-provider contract carries semantic requests and events without wire customization or response-metadata callbacks.
- FCT-08: Existing coded failure and retry behavior defines the current execution context in which extension customization would be used.

## Impact

Impact is HIGH for extension authors and their users because these model-facing behaviors are unavailable through the public extension boundary. Project-instruction and context-projection extensions cannot influence ordinary conversation requests. Input extensions cannot consume an image or complete a user interaction before the agent starts. Provider-request extensions cannot supply custom headers or request bodies, and provider-response extensions cannot inspect status and headers. A finalized-message extension cannot supply the final role-preserving message through the existing observer operation.

## Current state

The three client modes pass text through the shared run coordinator into Core. Core appends a text user message, projects history, and calls Host model execution. Host prepares conversation history and executes detached provider attempts with retry coordination. Concrete in-process adapters serialize those attempts and consume provider streams. Core persists terminal responses before message-end observation. Registration and dispatch support the existing operations described in the evidence section.

These paths, their persistence ordering, public failure delivery and existing test infrastructure are recorded once in [facts.md](facts.md).

## Desired state

Extension authors can supply the model-facing behavior described in the ticket through ordinary extensions. Users get the same extension behavior in each supported execution mode, with request-local customization distinct from durable conversation history.

## Problem boundary

This problem covers ordinary conversation input and model-facing customization, including shared execution used by extension-owned configured-model requests. Text/image processing includes inputs introduced or transformed by extensions, as well as inputs supplied by supported client contracts. Provider customization covers headers, serialized requests and response metadata.

Tool policy, run-continuation policy, extension commands, resource discovery, extension-defined provider installation and TUI presentation contributions belong to later phases. Existing compaction and retry policy remain their current owners' behavior.

## Assumptions

None.

## Open questions

None.
