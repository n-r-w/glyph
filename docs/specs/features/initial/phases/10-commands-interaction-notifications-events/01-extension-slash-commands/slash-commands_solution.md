# Technical solution: PHS-10.1 - Extension-defined slash commands

Status: Draft.

## Problem statement

The approved [problem statement](slash-commands_problem.md) defines the author-contribution and caller-access problems. The [PRD](slash-commands_prd.md) defines FRQ.01-FRQ.03 and SCN.01-SCN.04. The [parent phase](../ticket.md#scope-boundary) defines the exclusions.

## Decisions

- DEC.01: Command names are local to an extension. Different extensions can contribute the same name. The standard TUI supports a full address such as `/alpha:review`. A short form such as `/review` is available only when one registered extension command has that name and no implemented built-in TUI command uses it. For an ambiguous short form, TUI reports the available full addresses. For example, if `alpha` and `beta` both contribute `review`, `/review` is ambiguous and `/alpha:review` selects `alpha`.
- DEC.02: Host admits command invocations independently of agent runs and other commands. The receiving extension and the Host capabilities it calls retain their own admission rules.
- DEC.03: When editor input begins with `/`, the standard TUI automatically shows matching implemented built-in commands and extension commands with their purposes. Typing the command name filters the list. Choosing an extension command inserts its full address into the editor. Sending the edited input remains an explicit action. Argument completion is outside this solution.
- DEC.04: An unknown slash name follows the ordinary user-input path. For example, TUI submits `/reveiw changes.go` as an ordinary message when no command has that name. Once the TUI recognizes a command, an invocation failure remains a command failure and never becomes ordinary user input.
- DEC.05: Escape first closes a focused command list or dialog. Otherwise, the latest still-running extension command is the current work and receives cancellation. TUI shows its name as the interruption target. Other operations continue. When no extension command remains active, TUI returns to its ordinary interruption behavior, including stopping an active agent run. TUI exposes no cancellation command or operation-selection menu.

## Proposed solution

Host provides a command-contribution, discovery, and invocation mechanism. Extensions implement the actions, and each client owns its presentation and user-facing syntax. Agent Core receives no command-specific responsibility.

### SOL.01: Accepted command catalogue

Serves FRQ.01 and FRQ.02.

A command's stable identity combines its extension ownership with its local name. Host keeps accepted declarations in a process-local catalogue, separate from current runtime availability. Host therefore distinguishes an unknown command from a known command whose extension has become unavailable. TUI evaluates DEC.01 against the registered declarations, so it preserves short-name resolution when runtime availability changes.

The command capability owner validates contributions during ordinary extension startup. Local names must support unambiguous addressing, declarations must provide a purpose for discovery, and duplicate names within one extension are invalid. Cross-extension duplicates are valid. Invalid contributions follow the startup policy for invalid extension registration, with the full reason exposed through the existing startup reporting paths.

The existing `Service.Load` in `host/internal/usecase/host/startup/service.go` coordinates pending registrations, capability validation, capability commit, and runtime activation. Startup includes command contribution at this boundary. The runtime owner continues to manage executable discovery, registration transport, runtime generations, and availability. The command owner retains accepted command policy and routing information. Declaration validity is decided at contribution acceptance; transport boundaries validate message shape, and invocation checks current target availability.

Host reconstructs the catalogue from loaded extension contributions. It has no separate persistence or reload mechanism. The platform's environment-reload lifecycle remains authoritative when that capability is implemented.

### SOL.02: Discovery through both client contracts

Serves FRQ.02 and supports FRQ.03.

Host exposes discovery through UI Plugin Contract and Programmatic Control from the same catalogue. Host includes extension ownership and each command's local name, purpose, and availability in the discovery result. Host retains client-neutral command identity; the standard TUI constructs the slash notation in DEC.01.

Host includes the accepted command catalogue in UI initialization, so TUI has command identities before it accepts command input. TUI combines those contributions with its implemented built-ins. When opening completion, TUI uses its initialized catalogue and refreshes availability through the discovery operation. A delayed refresh cannot reopen dismissed completion or alter editor text. Invocation remains authoritative if availability changes after discovery. A Programmatic controller obtains the same extension-command information through discovery without loading a UI plugin.

The existing UI and Programmatic contracts already expose Host-owned queries through correlated operations. Discovery uses that operation infrastructure and requires no agent run or model request. While discovery requests run, TUI keeps its current interruption target.

### SOL.03: Explicit extension invocation

Serves FRQ.03.

Both clients can explicitly request invocation of one extension-owned command. At admission, Host captures the selected declaration, runtime generation, and issued active-session context for the operation. It invokes that target through a typed command capability on the Extension Contract. The SDK supplies the invocation context and operation lifecycle while the extension supplies the action.

The command capability shares the existing transport and operation infrastructure. It does not use user-input transformation or model-callable tool semantics. The current `Coordinator.RunPrepared` in `host/internal/usecase/host/runcontrol/coordinator.go` invokes user-input middleware before Agent Core. Using that route would retain the interception problem in PRB.04. Existing tool execution also has model visibility, tool-policy, and tool-result behavior that command invocation does not require.

Command recognition uses the original editor content before ordinary-message normalization. The TUI consumes the command address and its single separating whitespace character as command syntax; the remaining text is the argument text. Host and the SDK preserve that remainder, including its whitespace and line breaks, without shell parsing or interpretation. Omitted arguments reach the action as no argument text. For example, `/review changes.go` supplies `changes.go` to the action. The extension owns the meaning and validation of its arguments.

For command dispatch, Host starts no agent run and automatically persists no user message. The action can use its issued context to call configured models, access its session-bound data, and use other capabilities already available through the public Extension Contract. For the review example, the extension owns the review instructions, file interpretation, model use, and findings; Host supplies the existing model and session services.

Command-operation state remains separate from agent-run availability. A command's completion cannot mark a concurrently running agent as idle. The current `Service.prepareCommand` and `Service.Notify` in `plugins/ui/tui/internal/usecase/presentation/commands.go` track correlated pending operations. Extension invocations join that tracking with their own identities and outcomes.

### SOL.04: Admission, session binding, failure, and cancellation

Serves FRQ.03 and the shared operation requirements inherited by the PRD.

Each client request follows the existing asynchronous operation lifecycle. Before acceptance, Host performs only local shape, identity, readiness, and in-memory admission checks. Command readiness requires the selected runtime and an active-session binding. Model readiness belongs to the model operation if the action requests one. Extension invocation and other external work occur after acceptance. The extension may reject its own invocation, and a nested Host capability may reject unavailable admission; accepted parent work reports the resulting failure with its complete cause.

DEC.02 adds no whole-command reservation of the shared run/session gate. The current `Service.TryAcquire` in `host/internal/usecase/host/operationgate/service.go` is non-reentrant. Extension-initiated manual compaction acquires that gate in `Service.prepareCompactionAdmission` in `host/internal/controller/extension/context_admission.go`. Holding the gate for a complete command would obstruct that command's own compaction request. Independent command admission reuses capability-specific rules and avoids introducing inherited or reentrant reservations.

Host applies the existing context-validity rules. After session replacement or runtime replacement, Host rejects access through an old context. A command cannot silently transfer its session writes to the replacement session. A caller that needs work in a replacement session waits for confirmation of the replacement before invoking the command. For example, if a user starts a review and then creates a new session, Host rejects that review's attempts to save findings through its old context into the new session. Completed extension effects retain their existing semantics; Host performs no rollback of those effects when a command fails or is canceled.

The runtime owner binds command execution to its existing parent-invocation lifetime. SDK context requests remain children of that invocation. The current `Runtime.beginInvocation` in `host/internal/infra/plugins/extension/runtime/invocation.go` cancels and joins child work. The method `contextOperation.Run` in `host/internal/controller/extension/service.go` links a child to that lifetime. Operation owners apply the same cancellation and joining guarantees to targeted cancellation and connection/runtime shutdown. Terminal reporting occurs after the command and its owned work stop, as required by the shared operation contract.

The command owner defines command-level error meaning and preserves complete error text across the extension, Host, and either client contract. It does not borrow the public meaning of agent-run, tool-result, or compaction failures for unrelated command failures. When an action fails, Host fails its originating operation. When a runtime is lost, Host can fail every invocation using that runtime. Host performs no automatic command retry or reconnection replay; a caller can explicitly invoke an action again.

### SOL.05: Standard TUI interaction

Serves the TUI variants of FRQ.02 and FRQ.03.

TUI implements DEC.03 through the ordinary editor interaction. A slash inside an ordinary message does not open command completion. Completion covers command names and purposes, including full addresses needed to distinguish extension ownership. Filtering considers the local name and the full address. The standard TUI retains ownership of filtering, focus, selection, and editor restoration.

TUI resolves implemented built-ins locally and extension commands against its catalogue snapshot. TUI applies DEC.04 when submitting an unknown name. An ambiguous short name remains a command-resolution error, and a recognized unavailable command remains an addressed invocation failure. Neither condition is reclassified as an unknown message after a failed attempt.

DEC.05 defines interruption without a new command-management interface. TUI distinguishes every active invocation even when one registered command is called several times. It derives the visible interruption target from its pending command operations. Completion or cancellation of an older operation cannot clear the target of a newer one. With an agent run and `/alpha:review` active, Escape cancels the command and leaves the agent running. Once no extension command remains active, an ordinary interrupt targets the agent. Closing completion or a dialog restores the preceding editor content and focus before a later Escape can interrupt work.

An interrupted command retains its own cancellation outcome, and the agent and other commands keep their independent operation lifetimes. Provider or extension transport failure remains subject to the shared lifecycle and error rules. Programmatic controllers already own their operation identifiers and use the existing targeted cancellation operation directly.

### SOL.06: Package boundaries and dependency inversion

Serves FRQ.01-FRQ.03 under the project architecture constraints.

The command capability owner belongs in `host/internal/usecase/host/commands`. Its responsibilities are accepted contribution policy, catalogue access, target resolution, and logical invocation. Both client paths consume it, so it is a shared capability owner with concrete behavior. It owns no extension process, terminal widget, or action implementation.

Startup, Host UI, and Host Programmatic declare the interfaces and contract types they consume. The command owner implements those contracts. The command owner declares the runtime-invocation and context-access interfaces it consumes; `extensionruntime` and `extensioncontext` implement them. Consumer-owned registration and client projections stay with their consumers. They are not relocated into a shared contract package to avoid an import cycle.

This gives dependency directions from the command implementation toward its startup/client consumers, and from runtime/context implementations toward the command consumer contracts. Host UI and Programmatic do not import the concrete command implementation or use its implementation-owned types. The command owner does not import concrete runtime or context implementations. Application assembly binds concrete components.

The current compiler import graph was inspected, and these proposed dependency edges were simulated without a cycle. This is a check of the proposed boundaries, not compilation of unimplemented code. The project rule for any later cycle remains authoritative: inspect the full dependency chain and responsibility boundaries, then resolve the architectural cause.

The Extension Contract and SDK expose public types only. UI plugin and extension projects continue to compile independently of Host internals. No command types, dispatch, or command-specific interfaces enter Agent Core.

### Critical-scenario fit

| Scenario | Methods | Observable result |
|---|---|---|
| SCN.01 | SOL.01, SOL.02, SOL.05, SOL.06 | A separately delivered extension contributes commands through the public boundary, and the TUI user sees their names and purposes. |
| SCN.02 | SOL.01, SOL.02, SOL.06 | A controller discovers the same extension commands without loading the TUI. |
| SCN.03 | SOL.03, SOL.04, SOL.05 | Both clients invoke the explicitly selected extension action with unchanged optional arguments despite ordinary input handlers. |
| SCN.04 | SOL.03, SOL.04 | The extension completes the review and recovers its saved findings through its public session access. |

## Overengineering and overspecification considerations

The catalogue and routing owner are needed by contribution, discovery, and both invocation paths. Typed invocation removes the dependency on arbitrary user-input handlers. Existing operation ownership and context lifetimes provide asynchronous execution and cancellation. The TUI's pending operations provide interruption targeting without a new task manager.

The solution introduces no command scheduler, acceptance queue, whole-command session lock, automatic retry, persistent command registry, command-specific agent loop, generic extension-defined UI, argument-completion protocol, `/commands`, or `/cancel`. Command-provided questions, notifications, main-session control, and resource contribution remain with their owning phase boundaries.

The trade-off of independent execution is that extensions must admit their own concurrent work or reject it, and an active-session change can invalidate an unfinished command's Host access. The trade-off of DEC.05 is that cancelling an older command from the TUI requires newer active commands to finish or be interrupted first. A Programmatic controller can address any owned active invocation directly. These costs preserve the agreed interaction without adding an operation-selection interface.

Implementation details include exact protobuf declarations, SDK and Go signatures, transport validation details, error-category constants, file split, completion layout, and test fixtures.

## Open questions

None.

## References

- [Problem statement](slash-commands_problem.md): approved harms, current behavior, and phase boundary.
- [PRD](slash-commands_prd.md): approved requirements and critical scenarios.
- [Shared operation requirements](../../../../../issues/blocking-contract-operation-processing/prd.md): admission, lifecycle, cancellation, closure, and error semantics.
- [Target architecture](../../../architecture.md): Agent Core isolation, Host capability ownership, and consumer-owned contracts.
- [Standard TUI requirements](../../../standard-tui.md): command completion, selectors, interruption, and editor preservation.
- [TUI defaults](../../../tui-defaults.md): client-owned built-in command names and initial keybindings.
- `AGENTS.md`: project dependency-inversion and Pi-reference rules.
- `@earendil-works/pi-coding-agent` 1.5.0, `docs/usage.md` and `docs/keybindings.md`: reference interaction for Escape; no Pi architecture or algorithms are reused.
- [Claude Code interactive-mode documentation](https://code.claude.com/docs/en/interactive-mode): reference interaction for interruption and focused dialogs.
