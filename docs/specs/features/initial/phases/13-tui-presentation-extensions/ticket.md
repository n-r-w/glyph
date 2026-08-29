# Ticket: PHS-13 - Standard TUI presentation extensions

Let extensions add passive presentation to the standard TUI while the TUI retains terminal ownership.

## Key definitions and abbreviations

- DEF-01: Passive TUI contribution. Extension presentation that does not receive terminal focus or own the render loop.

## Problem Statement

- PRB-01: An extension cannot add passive presentation to the standard TUI. Replacing the complete UI plugin is the only available presentation customization.

## Target Picture

- SOL-01: Let extensions add passive presentation to the standard TUI while the TUI retains terminal ownership.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension user.
- Pre-condition: DEP-01 is met.
- Trigger: an extension publishes passive TUI presentation.
- Required behavior: the standard TUI displays the contribution while retaining terminal input, focus, and render-loop ownership.
- Example input and expected output: Input: let one extension publish footer text and another publish a tool-result renderer. Expected output: both appear in the standard TUI and disappear when their owning runtimes become unavailable.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No focused overlays, editor integration, themes, or extension shortcuts.

## Dependencies and Preconditions

- DEP-01: [PHS-12.3](../12.3-standard-tui-editor-terminal-interaction/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Let extensions add passive presentation to the standard TUI while the TUI retains terminal ownership.

### Functional Requirements

- FRQ-01: Add statuses, working indicators, widgets, headers, footers, terminal title, and hidden-reasoning labels.
- FRQ-02: Add renderers for tool calls, tool results, custom messages, and custom session entries.
- FRQ-03: Add tool-result expansion inspection and updates.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Standard TUI presentation-extension contract.
- DLV-02: Reference footer, status, widget, and renderer extensions.

### Acceptance Criteria

- ACC-01: Multiple extensions contribute passive presentation without taking terminal input or render-loop ownership.
- ACC-02: The standard TUI removes an extension's presentation when its runtime becomes unavailable.
- ACC-03: Using the same extension headlessly keeps core behavior active and returns explicit errors for attempted TUI-only operations.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Frequent extension updates can stall rendering. The TUI owns update scheduling and applies bounded presentation messages without granting render-loop control.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](../../delivery-plan.md) - ticket order and ownership.
- REF-03: [UI plugin process contract](../../../../../../api/plugins/ui/v1/ui.proto) - UI plugin process contract.
