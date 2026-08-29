# Ticket: PHS-14 - Interactive standard TUI extensions

Support focused extension interaction and editor integration inside the standard TUI.

## Key definitions and abbreviations

- DEF-01: Interactive TUI contribution. Focused extension content hosted by the standard TUI while the TUI retains terminal ownership.

## Problem Statement

- PRB-01: The standard TUI cannot host focused extension content, overlays, forwarded input, editor integration, themes, or configurable extension shortcuts.

## Target Picture

- SOL-01: Support focused extension interaction and editor integration inside the standard TUI.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: extension user.
- Pre-condition: DEP-01 is met.
- Trigger: an extension opens focused content and changes editor text.
- Required behavior: the standard TUI routes focused input, applies the editor change, and resumes normal input after completion.
- Example input and expected output: Input: invoke a structured-input command that opens an overlay and inserts the selected result into the editor. Expected output: only the focused overlay receives input and normal editor focus returns after completion.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No alternative UI plugin replacement or universal renderer shared by future UI plugins.

## Dependencies and Preconditions

- DEP-01: [PHS-13](../13-tui-presentation-extensions/ticket.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Support focused extension interaction and editor integration inside the standard TUI.

### Functional Requirements

- FRQ-01: Add custom areas, overlays, focus, and forwarded terminal input.
- FRQ-02: Add editor text read, replace, insert, autocomplete contribution, and editor component replacement.
- FRQ-03: Add theme contribution, enumeration, switching, and configurable TUI-specific shortcuts.

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Interactive TUI extension contract.
- DLV-02: Reference structured-input, overlay, editor, theme, and shortcut extensions.

### Acceptance Criteria

- ACC-01: An extension displays an overlay, receives input only while focused, and returns control to the standard TUI.
- ACC-02: An extension reads and changes editor text without owning terminal input outside its active interaction.
- ACC-03: Contributed themes and shortcuts use the same user configuration rules as built-in TUI actions.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: An extension component can fail while focused. The standard TUI must reclaim focus, remove the failed area, and remain usable.

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
