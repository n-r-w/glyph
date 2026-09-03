# Analysis Results: Extension events and UI consumption

This analysis records the current Glyph event paths, the unresolved relationship between extensions and UI plugins, candidate models, and the decisions required before event behavior is added to the product requirements.

## Scope

In scope:

- Events produced by Agent Core, Host, and extension processes.
- Event observation or transformation by extensions before client delivery.
- Delivery to UI plugins and Programmatic Control.
- UI implementations that depend on a known set of extensions.
- The relationship between event delivery and the planned standard-TUI extension phases.

Out of scope:

- Selection of a target event model.
- Protobuf fields, package placement, or implementation tasks.
- UI rendering rules and component APIs.
- Changes to the approved extension operation lifecycle.

## Key definitions and abbreviations

- Agent event. A provider-neutral lifecycle event produced by Agent Core during an agent run.
- Host operation result. A typed result or progress event produced by a Host-owned operation.
- Extension handler. An extension operation invoked by Host at a documented extension point.
- Extension-defined event. A candidate event whose semantics are owned outside Agent Core and Host. Glyph does not currently define this event category.
- Event processing. Observation, transformation, rejection, replacement, or another action applied before event delivery. Glyph does not currently define one general event-processing contract.
- Event delivery. Transfer of an event to a declared recipient. Delivery does not define presentation.
- UI consumption. Interpretation and presentation performed by a UI plugin after it receives data through the UI Plugin Contract.

## Executive Summary

Glyph has no general event model that connects Agent Core events, Host operation events, extension handlers, inter-extension events, and UI consumption. Current Agent Core events reach the active Glyph client without extension preprocessing. Current session-tree extensions participate in a Host operation before commit rather than processing a client event. The Extension Contract cannot initiate an arbitrary event or Host operation.

The target requirements plan inter-extension events, interactions, notifications, and standard-TUI extension capabilities, but they do not define how those concepts relate. The statement that a future UI plugin may expose its own extension-rendering contract has no corresponding Host or Extension Contract boundary. The PHS-13 and PHS-14 requirements also assign component, renderer, focus, and editor behavior before the event relationship is resolved.

No target event model is selected by this document. The open questions must be resolved before PHS-10 changes public event contracts and before PHS-13 or PHS-14 is implemented or removed.

## Background and Context

Glyph keeps Agent Core independent of UI and runs extension and UI plugins as separate local processes. The UI Plugin Contract is Host-owned. The Extension Contract is also Host-owned. A UI plugin cannot add a new Host operation or make Agent Core understand a UI-specific contract without a Glyph contract change.

A UI implementation can interpret known events and can be designed for a known set of extensions. This does not require one extension presentation to work across every UI implementation. It does require a defined path for the UI to receive the data on which it depends.

Pi demonstrates lifecycle events, extension communication, notifications, interactions, and in-process TUI component APIs. Pi's feature set is relevant to capability discovery, but its in-process TypeScript UI components do not establish a process contract for Glyph.

## Method and Data Sources

The analysis examined:

- Agent Core event production and Host event dispatch in `host/internal/usecase/agent/run` and `host/internal/usecase/host/events`.
- UI Plugin Contract event and operation messages in `api/plugins/ui/v1`.
- Programmatic Control event and operation messages in `api/programmatic/v1`.
- Extension operations and session-tree handlers in `api/plugins/extension/v1`, `host/internal/usecase/host/extensions`, and `host/internal/usecase/host/sessiontree`.
- Target requirements and planned phases in `docs/specs/features/initial/prd.md`, `architecture.md`, `delivery-plan.md`, and the PHS-07, PHS-10, PHS-13, and PHS-14 tickets.
- Pi feature evidence in `docs/artefacts/pi-extension-surface.md`.

The analysis does not infer product intent where the inspected sources provide no decision.

## Observations

- OBS-01: `host/internal/usecase/agent/run.EventSink` receives Agent Core lifecycle events. `host/internal/usecase/host/events.Dispatcher` forwards them to the delivery function selected by the application composition.
- OBS-02: UI mode maps Agent Core lifecycle events to `HostProgress.agent_event` in the UI Plugin Contract. The standard TUI decides how to present those typed events.
- OBS-03: Programmatic mode maps Agent Core lifecycle events to `HostProgress.agent_event` in Programmatic Control. Programmatic Control does not load a UI plugin.
- OBS-04: No extension handler participates in the current Agent Core event-delivery path before UI or Programmatic delivery.
- OBS-05: The Extension Contract currently carries only Host-initiated `Register`, `Handle`, `Execute`, and cancellation operations. `OpenResponse` carries lifecycle events for those Host-initiated operations and cannot initiate an arbitrary Host operation or publish an arbitrary event.
- OBS-06: Session-tree extensions process `session_before_tree` request and result extension points before Host commits navigation. The `session_tree` observer runs after commit. The UI receives the final navigation operation result rather than an extension-transformed UI event.
- OBS-07: Current Codex authorization uses the UI-specific `AuthorizationRequest` progress message and `RetryAuthenticationCommand`. This path is not a general extension or interaction event path.
- OBS-08: PHS-07 plans Agent Core lifecycle observation by extensions, but it does not plan extension-defined client events.
- OBS-09: PHS-10 plans commands, interface-neutral interaction requests, notifications, and non-persisted inter-extension events. It defines these as separate capabilities and does not define preprocessing before client delivery.
- OBS-10: The PRD requires the standard TUI to expose statuses, widgets, headers, footers, overlays, renderers, editor integration, themes, and shortcuts to extensions.
- OBS-11: The PRD states that a TUI-specific renderer need not work in another UI plugin and that future UI plugins may expose their own extension-rendering contracts. The architecture names no process path through which a UI plugin adds such a contract to Host or the Extension Contract.
- OBS-12: PHS-13 and PHS-14 use reference extensions created to exercise generic TUI capabilities. No current Glyph extension requires those capabilities.
- OBS-13: `Initialization.extensions` gives the selected UI plugin the loaded extension identifiers and tool names after extension startup. It does not define required-extension declarations or behavior after a required extension becomes unavailable.

## Analysis and Interpretations

- INT-01: Final event delivery and preprocessing are independent concerns. An event can have a UI recipient after one or more extensions observe or transform the source operation or event.
- INT-02: Allowing generic transformation of Agent Core or committed Host events can violate their authoritative meaning. A model-selection event cannot report a state different from the committed selection. The event model must distinguish immutable observations from transformable pre-delivery data.
- INT-03: Extension processing can occur at the owning Host operation before commit, as session-tree handling does today. This preserves one authoritative state owner and can remove the need to transform the later client event.
- INT-04: An extension-defined payload can remain opaque to Host only when the source, recipients, size limit, error semantics, and unsupported-payload behavior are defined outside that payload.
- INT-05: A UI designed for a known extension set does not require a universal renderer. The UI and extensions still need a common transport path or existing Host commands, interactions, notifications, and session data sufficient for that UI.
- INT-06: A UI plugin cannot unilaterally establish a new Host-supported contract. A UI-specific agreement is possible only over a generic Glyph routing contract or through operations already defined by Glyph.
- INT-07: Inter-extension events, interaction requests, notifications, Agent Core lifecycle events, and UI-specific data have different recipients and result semantics. Combining them into one universal event bus is not justified by current requirements.
- INT-08: Sending every extension-defined event to Programmatic Control has no identified product scenario. Programmatic Control already has requirements for extension commands, interactions, notifications, and correlated operation events.
- INT-09: PHS-13 and PHS-14 cannot be evaluated until the product decides whether standard-TUI extensibility means generic runtime contributions or standard-TUI support for events from named extensions.

## Hypotheses and Tests

- HYP-01: Existing commands, interactions, notifications, session entries, and Host lifecycle events are sufficient for a UI that depends on known extensions. Test by defining one real UI and extension scenario and mapping every required input, output, state change, and failure to an existing or planned contract. Any unmapped data flow falsifies the hypothesis.
- HYP-02: Extension preprocessing belongs at named Host operation boundaries rather than in a general client-event pipeline. Test by mapping the same scenario to an owning operation before commit. A required transformation that has no owning Host operation falsifies the hypothesis.
- HYP-03: Standard-TUI runtime component registration is unnecessary for the initial product. Test against named extension integrations. A required integration that cannot be implemented through typed events, commands, interactions, notifications, or session data falsifies the hypothesis.
- HYP-04: A UI can validate its required extension set from `Initialization.extensions`. Test startup with every required extension present, one required extension absent, and one required extension becoming unavailable after initialization. The last case determines whether startup validation alone is sufficient.

## Options and Trade-offs

- OPT-01: Use only typed Host events, commands, interactions, notifications, and session data. A UI supports named extensions by understanding the data already exposed through Glyph. This has the smallest contract but cannot carry extension-specific data that has no existing typed projection.
- OPT-02: Add an opaque extension-to-UI data envelope. Host owns provenance, limits, delivery, and errors while the UI and source extension own payload semantics. This supports UI and extension pairs without a universal renderer, but it adds compatibility and unsupported-event rules.
- OPT-03: Add typed Glyph contracts for each new extension-to-UI event family. This preserves generated type safety and explicit semantics but requires a Glyph contract change for each new event family.
- OPT-04: Add a generic UI component or renderer protocol. This allows runtime presentation contributions but makes Glyph define layout, focus, rendering, update, compatibility, and failure semantics across a process boundary. No current scenario requires this cost.
- OPT-05: Let extensions preprocess client-bound data through named Host extension points. This follows the session-tree pattern and preserves typed ownership, but each preprocessing need requires a defined operation and handler contract.
- OPT-06: Add a general transformable client-event pipeline. This gives broad interception but risks changing authoritative Host events and duplicates operation-level extension points.

## Recommendation

No event option is selected. Resolve the open questions using one named UI and extension scenario before changing the PRD or a public event contract. Prefer an existing typed Host operation boundary when it carries all required data. Add a new event path only for a data flow that the scenario cannot express through existing commands, interactions, notifications, session data, or lifecycle events.

## Action Plan

- ACN-01: Define one concrete UI that depends on a concrete extension set, including startup, normal operation, extension failure, and shutdown.
- ACN-02: Map the scenario to current and planned Host operations and record each missing data flow.
- ACN-03: Resolve QST-01 through QST-08 before PHS-10 technical design changes public contracts.
- ACN-04: Re-evaluate the PRD TUI Extension Capabilities section and PHS-13 and PHS-14 after the event model is selected.

## Assumptions

- ASM-01: No released external Glyph plugin contract requires preservation because the project rules require no backward compatibility. Verify this assumption before the first external release.

## Open Questions

### QST-01: Which source event families need extension preprocessing?

- Impact: Determines whether PHS-07 lifecycle observation is sufficient or a later transformation path is required.
- Required answer: A closed list of Agent Core, Host operation, and extension-defined event families, with observer or transformer semantics for each.
- Evidence checked: Current Agent Core delivery and session-tree operation handling use different paths.
- Resolution point: Before PHS-10 technical design.

### QST-02: What can an extension change before UI delivery?

- Impact: Determines whether source identity, event type, recipients, correlation, and payload are immutable or replaceable.
- Required answer: A closed action set for each event family, including error and cancellation behavior.
- Evidence checked: The target architecture requires every extension point to declare observer, transformer, gate, or replaceable semantics.
- Resolution point: With QST-01.

### QST-03: Does a real UI and extension pair require extension-specific data outside existing contracts?

- Impact: Determines whether OPT-01 is sufficient or an extension-to-UI event path is needed.
- Required answer: One concrete scenario with the exact missing data and its producer and consumer.
- Evidence checked: PHS-10 already plans commands, interactions, notifications, and inter-extension events.
- Resolution point: Before adding a new public event type.

### QST-04: How does a UI declare or enforce its extension dependencies?

- Impact: Determines startup failure behavior and behavior after a required extension becomes unavailable.
- Required answer: Whether dependencies are documentation-only, checked during `Initialization`, or declared before UI selection, plus the exact failure behavior.
- Evidence checked: `Initialization.extensions` is available only after UI selection and extension startup.
- Resolution point: Before a UI depending on named extensions is implemented.

### QST-05: Which extension-originated information must reach Programmatic Control?

- Impact: Prevents accidental expansion of Programmatic Control into a generic extension event consumer.
- Required answer: A closed list based on programmatic scenarios. Existing requirements already include commands, interactions, notifications, and correlated operation results.
- Evidence checked: No current requirement sends arbitrary extension-defined events to Programmatic Control.
- Resolution point: Before PHS-10 contract design.

### QST-06: What is the delivery result for unsupported UI data?

- Impact: Determines whether an unknown extension event is ignored, reported, rejected, or closes the UI connection.
- Required answer: One observable result for publication and one required UI behavior.
- Evidence checked: Current UI protocol validation treats unknown protocol variants as errors, while no extension-defined event exists.
- Resolution point: Only when an extension-to-UI data path is selected.

### QST-07: Does extension-specific UI data need replay or only live delivery?

- Impact: Determines whether state belongs in session entries, Host queries, extension queries, or transient events.
- Required answer: Required behavior after UI startup, session resume, environment reload, and temporary UI unavailability.
- Evidence checked: Current Agent Core events are live operation progress, while session entries are persisted.
- Resolution point: With the first concrete UI and extension scenario.

### QST-08: What replaces the generic TUI component requirements?

- Impact: Determines whether PHS-13 and PHS-14 are removed, narrowed to named integrations, or retained with a new justified contract.
- Required answer: The concrete standard-TUI behavior and extension scenarios that require platform work.
- Evidence checked: Current tickets define reference extensions only and no production extension consumer.
- Resolution point: Before PHS-12.1 completes the standard-TUI foundation.

## References

- `docs/specs/features/initial/prd.md` - target product requirements, including Glyph clients and TUI extension capabilities.
- `docs/specs/features/initial/architecture.md` - process, ownership, event, and public contract boundaries.
- `docs/specs/features/initial/phases/07-extension-context-lifecycle/ticket.md` - planned lifecycle observation by extensions.
- `docs/specs/features/initial/phases/10-commands-interaction-notifications-events/ticket.md` - planned commands, interactions, notifications, and inter-extension events.
- `docs/specs/features/initial/phases/13-tui-presentation-extensions/ticket.md` - planned passive standard-TUI contributions.
- `docs/specs/features/initial/phases/14-interactive-tui-extensions/ticket.md` - planned interactive standard-TUI contributions.
- `docs/artefacts/pi-extension-surface.md` - Pi capability comparison and interface-ownership findings.
- `api/plugins/extension/v1` - current Extension Contract sources.
- `api/plugins/ui/v1` - current UI Plugin Contract sources.
- `api/programmatic/v1` - current Programmatic Control sources.
- `host/internal/usecase/agent/run` - Agent Core event source.
- `host/internal/usecase/host/events` - current Host event dispatch.
- `host/internal/usecase/host/sessiontree` - current operation-level extension composition.
