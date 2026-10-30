# PHS-08 target picture

## Goal

Implement [PHS-08](ticket.md) in full. Extension authors can register ordered middleware for system instructions, request-local context, user text and images, finalized messages, provider headers, serialized provider requests, and provider-response observation. The ticket remains the authoritative requirements and acceptance source.

## Acceptance scenarios

- A4: A reference extension changes effective system instructions through Programmatic Control and the standard TUI. Another transformation changes outbound context while persisted session entries remain unchanged.
- A5: Handler A appends marker `A`. Handler B receives both the immutable original value and A's current result, then replaces the current value with the original plus marker `B`. The outbound request contains `B` without `A`; persisted history contains neither marker.
- A6: An input extension transforms or fully handles text and image input. Host validates the final result against the selected model's input modalities. A text-and-image model accepts a final image. A text-only model rejects a final image before an agent run or provider request starts.
- A7: A reference extension changes a provider request and observes its response. A handler error or invalid action stops the affected operation before later handlers run, exposes the Glyph category and complete error text, and causes no recovery retry. A failure before dispatch prevents the provider request.

All ticket acceptance criteria, including Agent Core and provider independence, remain required in addition to these scenarios.

## Decision authority

The implementation may change public protobuf contracts, SDKs, Host code, Agent Core's minimal consumer-owned effective-context interface, tests, reference extensions, and related documentation. Host owns dispatch, ordering, transport mapping, failure mapping, and final-value validation. Agent Core consumes provider-neutral final values. Provider middleware coordination remains independent of concrete providers. Request-local transformations do not change persisted session history.

Technical contract shapes, package placement, implementation details, and domain terminology may be selected autonomously within the ticket. Record these decisions in `decisions.md` in this directory. Local commits for completed implementation units are allowed during implementation. Do not push or change remote refs.

Package or dependency changes need separate approval. Backward compatibility, fallback to an old implementation, and scope growth require separate approval. Tool middleware, extension commands, and TUI contributions are outside this phase. Fixes outside the ticket are governed by the workflow's limited authority for defects in files changed by this work.

## Verification policy

Use `final_only`: verify implementation units during delivery and present the combined result for final acceptance. Use focused RED/GREEN behavioral tests and all mandatory project checks: `task fmt`, `task fix_dry_run` with proposal analysis and appropriate fixes, `task lint`, `task test`, `task itest`, and `task test-coverage`. Check architecture boundaries and report actual verification limits explicitly.

## Task directory

`docs/specs/features/initial/phases/08-prompt-context-input-provider-middleware`

## Open questions

None. Target picture, acceptance scenarios, decision authority, task directory, and `final_only` verification were confirmed through option O2-1.
