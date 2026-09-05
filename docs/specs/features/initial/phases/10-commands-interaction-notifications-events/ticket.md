# Ticket: PHS-10 - Commands, interaction, notifications, and extension events

Let extensions expose user actions and request Host or client behavior.

## Key definitions and abbreviations

- DEF-01: Interaction request. An extension request sent through Host to a Glyph client that expects a response.
- DEF-02: Provider authentication operation. A client-neutral Host operation that asks one configured provider to start or retry its authentication flow.

## Problem Statement

- PRB-01: Extensions cannot register client commands, request user interaction, send notifications, or exchange transient events.
- PRB-02: The implemented Codex authentication flow uses `RetryAuthenticationCommand` and `AuthorizationRequest` from the UI Plugin Contract. Programmatic Control has no equivalent authentication operation, and the provider-specific UI messages cannot support the target extension-owned provider path.

## Target Picture

- SOL-01: Let extensions expose user actions and request Host or client behavior. Replace the provider-specific UI authentication path with one client-neutral Host operation whose provider interaction uses the same interface-neutral interaction contract.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension user.
- Pre-condition: DEP-01 is met.
- Trigger: the user invokes an extension command that requests input and a model query.
- Required behavior: the active client returns the interaction result and the extension receives its configured-model result.
- Example input and expected output: Input: invoke extension command `review`, answer its interaction request with `strict`, and let it query a configured model. Expected output: the command receives `strict` and returns the model result through the invoking client.

### SCN-02: Client-neutral provider authentication

- Actor: Glyph client user.
- Pre-condition: DEP-01 is met and one configured provider requires interactive authentication.
- Trigger: the user starts or retries authentication through the active Glyph client.
- Required behavior: Host invokes the selected provider authentication operation, routes each provider interaction through the active Glyph client, and returns the same semantic result through UI Plugin Contract and Programmatic Control.
- Example input and expected output: Input: start Codex authentication through each Glyph client kind after stored authentication fails. Expected output: both clients receive the same interface-neutral interaction request and terminal authentication result without a Codex-specific UI message.

### SCN-03: User-command session handoff

- Actor: Glyph user.
- Pre-condition: A session-aware extension command is registered and the active session contains work to continue separately.
- Trigger: The user invokes the extension command through a Glyph client.
- Required behavior: The command creates a replacement session through Host and carries selected context into that session through a newly bound extension context.
- Example input and expected output: A `handoff` command takes the user's next goal and selected prior context. Host creates a new active session, retains the source session, and the command stores the context in the replacement session without using stale session-bound objects.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No resource contributions, provider registration, or TUI component contribution.
- OSP-02: No automatic session creation or switching initiated by ordinary lifecycle handlers and no built-in child-agent or workflow semantics.

## Dependencies and Preconditions

- DEP-01: [PHS-09](../09-tool-middleware-run-control/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Let extensions expose user actions and request Host or client behavior.

### Functional Requirements

- FRQ-01: Add command registration, discovery, invocation, and provenance through Glyph clients.
- FRQ-02: Add interface-neutral interaction requests and notifications with explicit unavailable-client and delivery-failure results.
- FRQ-02.1: An unavailable-client or delivery-failure result shall contain a closed Glyph category and complete error text. Equivalent failures shall expose the same category and information completeness through UI Plugin Contract and Programmatic Control.
- FRQ-02.2: Add one client-neutral provider authentication operation to UI Plugin Contract and Programmatic Control. The operation shall identify one configured provider and shall start or retry that provider's authentication without embedding provider-specific fields in either client contract.
- FRQ-02.3: Every interactive step requested by a provider authentication implementation shall use the interface-neutral interaction contract. The Host authentication use case and interaction routing shall import no concrete provider package.
- FRQ-02.4: Remove `RetryAuthenticationCommand` and `AuthorizationRequest` from the UI Plugin Contract after the client-neutral provider authentication operation and interaction path have working producers and consumers.
- FRQ-02.5: Rejection, interaction-delivery failure, cancellation, and terminal authentication failure shall expose closed Glyph categories and complete error text with equal semantic meaning through UI Plugin Contract and Programmatic Control.
- FRQ-03: Add non-persisted inter-extension events.
- FRQ-04: Allow extension commands to use the configured-model request contract delivered by PHS-07 without changing the active conversation model or reasoning choice.
- FRQ-05: During a user-invoked extension command, the extension shall be able to initiate and cancel session creation, resumption, forking, cloning, and tree navigation through the public Extension Contract. Host shall retain ownership of admission, cancellation, navigation, persistence, and client results.
- FRQ-06: Session-control operations initiated by a command shall follow the Host operation rules used by Glyph clients. Rejection shall change no session state. Cancellation shall expose the actual terminal outcome without undoing a committed session transition. A command shall receive a newly bound extension context after session replacement; the preceding context shall remain stale.
- FRQ-07: Session control shall work through both client kinds without the extension importing Host internals or using a second client connection to control Host.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Command registry and invocation contracts.
- DLV-02: Interaction, notification, and extension-event contracts.
- DLV-03: Reference command, interaction, notification, and model-query extensions.
- DLV-04: Client-neutral provider authentication operation and migration of Codex authorization to the interface-neutral interaction contract.
- DLV-05: Command-initiated session operations and a reference handoff command through the public Extension Contract.

### Acceptance Criteria

- ACC-01: The same extension command is discoverable and invokable through the standard TUI and Programmatic Control.
- ACC-02: An interaction request succeeds through either connected client kind and fails without a client with its defined Glyph category and complete error text.
- ACC-03: Notification success means Host transferred it to the client and does not require presentation or a user response. Delivery failure retains the same Glyph category and complete error text through UI Plugin Contract and Programmatic Control.
- ACC-04: An extension model request uses a configured model without changing the active conversation model.
- ACC-05: UI Plugin Contract and Programmatic Control can each start or retry authentication for the same configured provider and receive equivalent interaction, cancellation, success, and failure semantics.
- ACC-06: Codex authorization uses the interface-neutral interaction contract, and production code contains no `RetryAuthenticationCommand`, `AuthorizationRequest`, or Host interaction dependency on a concrete Codex package.
- ACC-07: A real external extension command initiates creation, resumption, forking, cloning, and tree navigation through the Extension Contract. Each operation preserves its Host-defined session and branch semantics. The command works when invoked through either standard TUI or Programmatic Control.
- ACC-08: The handoff scenario retains the source session and writes selected context only into the replacement session. The new extension context works; an operation through the preceding context returns `STALE_CONTEXT` with complete error text.
- ACC-09: A command cancels an accepted session-control operation and observes its terminal outcome. A pre-commit cancellation changes no session state, and a committed transition is not undone. A busy rejection changes no session state and retains its category and complete error text.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: A client disconnect can leave an extension request pending. Host completes every pending interaction with a delivery error when the client connection closes.
- RSK-02: Removing the UI-specific authentication command without a client-neutral replacement would prevent recovery after failed startup authentication. FRQ-02.2 requires the replacement operation before removal.
- RSK-03: A command can retain a context for the session it replaced. FRQ-06 requires a fresh binding before post-switch work so that the command cannot append to the wrong session.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [prototype Host interaction path](../../../../../../host/internal/usecase/host/interactions/service.go) - prototype Host interaction path.
- REF-04: [UI event-model analysis](../../../../../artefacts/ui-event-model-analysis.md) - unresolved relationships between Host events, extension processing, and UI consumption.
