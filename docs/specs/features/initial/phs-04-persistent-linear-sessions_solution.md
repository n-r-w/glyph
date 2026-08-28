# Technical Solution: PHS-04 Persistent Linear Sessions

## Problem Statement

- PRB-01: `run.Service` stores conversation history only in process memory. Process exit removes user messages, terminal model responses, tool calls, tool results, and provider context.
- PRB-02: Programmatic Control and the UI plugin contract have no operations for session creation, listing, resume, naming, information, entries, or statistics.
- PRB-03: Glyph has no session domain, versioned session format, session storage adapter, or application restore path.
- PRB-04: Persistent history must retain provider-neutral text, images, model outcomes, tool data, usage, opaque provider context, and extension entry envelopes without adding persistence or provider SDK dependencies to Agent Core.
- PRB-05: Session accounting must retain message and tool counts, normalized token usage, and estimated USD cost. Reasoning tokens are a subset of output tokens and must not be counted twice.

## Proposed Solution

### Solution overview

- SOL-01: Add a Host-owned session domain, an active-session service, and a client session-control orchestrator. The active service owns persisted state. The orchestrator owns client operations and coordinates run start against session replacement.
- SOL-02: Store each session as one append-only, versioned JSONL file in the canonical working directory's digest partition below `~/.glyph/sessions`.
- SOL-03: Replace the in-memory history slice in `run.Service` with a consumer-owned `HistoryStore` interface. Agent Core appends only terminal provider-neutral history entries and reads immutable history snapshots.
- SOL-04: Persist each user entry before the provider request, each terminal model response before client completion and tool execution, and each terminal tool result before another model request.
- SOL-05: Add session operations to Programmatic Control and the UI plugin contract. The standard TUI implements `/new`, `/resume`, `/name`, and `/session` without changing transcript layout or terminal interaction outside session behavior.
- SOL-06: Add per-model USD pricing with present and absent states. Provider drivers normalize token usage. The active-session service calculates cost when it appends a terminal model response and persists the calculated cost with that response.
- SOL-07: Keep session accounting extensible through later owning stages. PHS-05 adds branch-summary usage, PHS-06 adds compaction, retry, and context-window accounting, and PHS-12.1 presents all available values in the rebuilt TUI layout.

### Terms and ownership

- ENT-01: `session.ID` is the opaque public identifier of one persisted session.
- ENT-02: `session.Header` contains the format version, session ID, creation time, and canonical working directory.
- ENT-03: `session.Entry` is one ordered terminal record. PHS-04 entry kinds are user, model, tool result, session information, and extension envelope. Each kind has one matching `mo.Option` payload and all other payloads are absent.
- ENT-03.1: A model session entry contains the provider-neutral `model.Response` and estimated-cost presence and value. Estimated cost is session data and does not enter Agent Core history.
- ENT-04: `session.ExtensionEnvelope` contains an extension identifier, extension-owned entry type, and opaque JSON value. PHS-04 stores and restores the envelope but adds no extension session API.
- ENT-05: `session.Info` contains session ID, user-name presence and value, canonical working directory, storage-path presence and value, creation time, and update time.
- ENT-05.1: The active-session service has internal write states `writable` and `write-unavailable`. This state is not persisted because resume derives a fresh state by validating storage.
- ENT-06: `session.Summary` adds the first user text used by a client when the user name is absent and the total-message count defined by ENT-07.
- ENT-07: `session.Statistics` contains user-message, model-response, tool-call, tool-result, and total-message counts. Total messages equal user messages plus model responses plus tool results.
- ENT-07.1: Statistics contain uncached-input, output, cache-read, cache-write, reasoning, and total token values. Token totals and reasoning detail share the availability rule in APC-19.
- ENT-07.2: Statistics contain estimated-cost availability and value plus cost breakdown by provider and model.
- CMP-01: `host/internal/domain/session` owns provider-neutral session entities. It has no JSON, protobuf, gRPC, provider SDK, filesystem, settings, or TUI dependency.
- CMP-02: `host/internal/usecase/host/sessions` owns active-session state, persistence-backed operations, its repository interface, and the Agent Core consumer-owned history implementation.
- CMP-02.1: `host/internal/usecase/host/sessioncontrol` orchestrates create, resume, naming, listing, information, entries, and statistics. It owns the active-session and operation-gate interfaces that it consumes.
- CMP-02.2: `host/internal/usecase/host/operationgate` owns one process-local exclusive gate shared by agent-run execution and session replacement. It implements separate minimal interfaces owned by `events` and `sessioncontrol` and does not lock session files across processes.
- CMP-03: `host/internal/infra/persistence/sessions` owns JSONL DTOs, filesystem paths, encoding, decoding, append, discovery, and crash-tail recovery. It implements the repository interface owned by the active-session use case.
- CMP-04: `host/internal/usecase/agent/run` owns the `HistoryStore` interface and the order in which terminal history becomes durable relative to provider and tool actions.
- CMP-05: Programmatic Control and UI consumer packages own their minimal session-control interfaces and transport-independent method types. Controllers own transport mapping and never pass protobuf values into the session-control orchestrator.
- CMP-06: `host/internal/app` constructs the session repository, active-session service, operation gate, Agent Core, events coordinator, and session-control orchestrator in that order for Programmatic Control, UI, and one-shot headless compositions.

### Session domain and Agent Core boundary

- APC-00.1: The active-session use case owns a `Repository` interface with initialization, append, list, and load operations expressed through use-case-owned commands and results.
- APC-00.2: The session-control orchestrator owns an `ActiveSessions` interface for create, resume, name, list, information, entries, and statistics. The `events` and `sessioncontrol` packages each own a minimal gate interface with `TryAcquire() (release func(), acquired bool)`. A successful release function is idempotent.
- APC-01: `run.HistoryStore` exposes `Snapshot() []agent.HistoryEntry` and `Append(context.Context, agent.HistoryEntry) error`.
- APC-02: `run.Service.History` and `run.Service.ProjectHistory` read from `HistoryStore`. `run.Service` no longer owns a second canonical history slice.
- APC-03: The active-session service maps user, model, and tool-result session entries to `agent.HistoryEntry`. Session-information and extension-envelope entries do not enter Agent Core history in PHS-04.
- APC-04: `HistoryStore.Snapshot` and all session query results return independent copies of byte slices, maps, slices, and `mo.Option` values.
- APC-05: `events.Coordinator.PrepareRun` uses nonblocking operation-gate acquisition before a Programmatic Control acceptance response. `events.Coordinator.Run` acquires the same gate before direct UI or headless execution. Failed acquisition returns busy and starts no run.
- APC-05.1: A prepared Programmatic Control run retains the gate reservation until `RunPrepared` settles. `events.Coordinator.CancelPrepared(runID)` cancels the prepared run when acceptance-response delivery fails before `RunPrepared` starts. The coordinator releases the reservation on every terminal path.
- APC-05.2: The session-control orchestrator uses nonblocking acquisition for create and resume. It holds the gate through repository load, validation, and active-state replacement. Failed acquisition returns busy and leaves the active session unchanged.
- APC-06: A process without an explicitly resumed session creates one in-memory empty session. The repository creates its file when the first user, model, tool-result, session-information, or extension entry is appended.
- APC-06.1: The active-session use case owns a `PricingCatalog` interface that returns pricing by configured provider ID and model ID. The Host provider catalogue implements this consumer-owned interface.
- CNS-01: Agent Core does not import `domain/session`, JSON DTOs, filesystem adapters, settings, protobuf, gRPC, provider SDKs, pricing calculations, or TUI packages.
- CNS-02: Session resume does not alter the active provider, model, or reasoning choice. Provider drivers apply the PHS-03 provider-context compatibility rules on the next request.

### Persistence order and run failure behavior

- STP-01: Agent Core atomically reserves the idle run state and emits `agent_start`. It then calls `HistoryStore.Append` for the user entry and starts no turn or provider request until the append succeeds.
- STP-01.1: User-entry append failure finishes the accepted run with failed `agent_end` and `agent_settled` events, returns Agent Core to idle, and releases the operation gate. The run emits no turn or message event.
- STP-02: A terminal model response has normalized usage when Agent Core passes it to `HistoryStore.Append`. The active-session service calculates cost and persists the entry before `message_end`, tool execution, or the next provider request.
- STP-03: Each terminal tool result is appended after tool execution and before `tool_execution_end`, `tool_result`, or another provider request.
- STP-04: A successful append writes one entry record, calls `File.Sync`, closes the file, and then updates the active in-memory snapshot. The first append writes the header and first entry in one buffer before the same sync and close sequence. Write, sync, or close failure leaves the active snapshot unchanged.
- STP-05: An append failure ends the active run with client-visible text equal to the complete returned persistence error, including its wrapping context and underlying cause. Agent Core performs no later provider request or tool execution that depends on the failed entry.
- STP-06: A tool can complete its external effect before persistence of its result fails. Glyph reports the persistence failure and performs no next provider request. Glyph does not claim to roll back the tool effect.
- STP-07: Abort and provider failure remain terminal model outcomes. After resume, `ProjectHistory` excludes failed model responses and supplies temporary skipped results for model tool calls without stored results.

### JSONL format and storage paths

- DEC-01: Each session uses one JSONL file. SQLite, a shared index, a daemon, and interprocess session locking are not added.
- DEC-02: Session format version 1 is a linear record sequence. The first line is one header and every later line is one complete entry.
- DEC-03: Every stored entry has a unique opaque entry ID and an RFC 3339 timestamp. The reader rejects duplicate entry IDs. File order is the PHS-04 conversation order.
- DEC-04: A session name change appends a `session_info` entry. The latest `session_info` entry owns the name. Glyph does not generate or persist an automatic name.
- DEC-05: User and tool images are encoded as base64 by the storage DTO. Provider-context payload bytes are encoded as base64 and remain opaque. Extension data is embedded as a compact JSON value; storage preserves its JSON meaning rather than source formatting or byte representation.
- DEC-06: Model tool calls remain ordered content inside the terminal model response. Tool results remain separate entries linked by the provider tool-call ID.
- DEC-07: The repository root is `~/.glyph/sessions`. Glyph obtains the canonical working directory with absolute-path resolution, symbolic-link evaluation, and path cleaning. Its SHA-256 digest maps to one fixed session directory, and the header retains the canonical path.
- DEC-07.1: A session filename contains its creation timestamp and generated session ID. Resume resolves an ID from validated headers in the working-directory session directory and never treats a client ID as a filesystem path.
- DEC-07.2: Creation time comes from the header. Update time equals creation time for an empty active session and the latest stored entry timestamp otherwise. Filesystem modification time does not change session ordering.
- DEC-08: Session directories use mode `0700` and session files use mode `0600`. Open validates a regular file and reapplies mode `0600` before reading.
- DEC-09: A new file is created with exclusive creation and receives the header and first entry before it becomes the active persisted file. Later records use append mode. Every append performs one write containing one encoded record and newline, calls `File.Sync`, and closes the file before success.
- DEC-10: The reader requires a header with type, version, ID, creation time, and canonical working directory, exact version 1, known record kinds, unique entry IDs, required fields, and no unknown fields. Unsupported versions and malformed newline-terminated records fail resume without modifying the file.
- DEC-11: Nonempty final bytes without a terminating newline are treated as an interrupted append after every preceding record passes DEC-10. List ignores that tail without modifying the file. Resume truncates only that tail, calls `File.Sync`, closes the file, logs one structured warning without persisted content, and then opens the preceding entries.
- DEC-12: Concurrent writers to one session are unsupported. PHS-04 adds no lock, lease, merge, or conflict detection.

The storage DTO uses this record shape. Field details for nested provider-neutral values follow their domain fields and do not import provider DTOs. `estimatedCost` is omitted when cost is unavailable and contains all five cost values when present.

```json
{"type":"session","version":1,"id":"...","createdAt":"...","cwd":"..."}
{"type":"user","id":"...","createdAt":"...","message":{"content":[]}}
{"type":"model","id":"...","createdAt":"...","response":{},"estimatedCost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}}
{"type":"tool_result","id":"...","createdAt":"...","result":{}}
{"type":"session_info","id":"...","createdAt":"...","name":"Refactor auth module"}
{"type":"extension","id":"...","createdAt":"...","extensionId":"...","entryType":"...","data":{}}
```

### Session operations

- APC-07: The session-control orchestrator acquires the operation gate, calls active-session `Create`, replaces the active session with a new empty session, and returns its `session.Info`.
- APC-08: `List` returns persisted files that pass DEC-10, with the non-mutating interrupted-tail rule from DEC-11, for the canonical process working directory. Results are ordered by update time descending and then session ID ascending.
- APC-09: The session-control orchestrator acquires the operation gate, calls active-session `Resume`, loads the complete file, validates its canonical working directory, and atomically replaces the active session.
- APC-10: `SetName` accepts a nonempty name after trimming. It replaces CR and LF runs with one space, appends a session-information entry, and returns updated information. Naming does not replace the session and remains available during a run.
- APC-11: `Info` returns active session information. `Entries` returns ordered client-visible terminal entries. `Statistics` derives values from all stored entries, not only provider-visible projected history. List and query operations remain available during a run.
- APC-12: A session without a user name exposes its first nonempty user text in `session.Summary`. A summary with no user text uses the session ID for display. Clients shorten display text and do not persist it as a name.
- APC-13: `GetMessages` remains the active provider-neutral public conversation query. `GetSessionEntries` adds entry IDs, timestamps, terminal outcomes, usage, and cost without exposing provider context.
- APC-14: PHS-04 does not expose extension-envelope payloads through Glyph client queries. PHS-07 defines model-hidden and model-visible extension entry behavior before client projection changes.

### Token accounting and pricing

- ENT-08: `model.Usage` uses normalized disjoint token buckets for uncached input, output, cache read, and cache write. Reasoning tokens are a reported subset of output tokens. Total tokens equal uncached input plus output plus cache read plus cache write.
- ENT-09: `model.Response.Usage` is `mo.Option[model.Usage]`. Absence means usage is unavailable. A present usage value can contain zero counts.
- ENT-10: `model.Descriptor.Pricing` is `mo.Option[model.Pricing]`. `model.Pricing` contains flat USD rates per 1,000,000 tokens for input, output, cache read, and cache write. It has zero or more ordered request-wide tiers.
- ENT-11: A pricing tier contains an exclusive input-token threshold and all four rates. Request input equals uncached input plus cache read plus cache write. The highest tier whose threshold is lower than request input applies to the complete request.
- ENT-12: `session.EstimatedCost` contains `float64` input, output, cache-read, cache-write, and total USD values. It is present only when both usage and pricing are available. Tests compare calculated values with an explicit floating-point tolerance.
- APC-15: OpenAI Responses and Chat Completions adapters set uncached input to `max(0, provider input minus cache read minus cache write)`. They retain nonnegative provider cache buckets and compute total from the four normalized buckets.
- APC-16: Output tokens retain provider semantics and already include reasoning tokens. Cost calculation never adds reasoning tokens separately.
- APC-17: The active-session service resolves pricing by the configured provider ID and requested model ID stored in the terminal response. Provider-returned response-model metadata does not select pricing.
- APC-18: The active-session service calculates estimated cost inside `HistoryStore.Append` before repository append. Input, output, cache-read, and cache-write costs equal their token bucket multiplied by the matching rate and divided by 1,000,000. Total cost is the sum of those four costs. The persisted response cost remains unchanged after settings changes.
- APC-19: Session token totals are available only when every stored model-response entry has usage. Session estimated cost is available only when every stored model-response entry has persisted cost. A session with zero stored model-response entries has available zero token totals, available zero estimated cost, and an empty provider-model cost breakdown. Counts remain available independently.
- APC-20: Cost breakdown groups persisted costs by configured provider ID and requested model ID, ordered by provider ID and then model ID. Each group has its own available or unavailable state under APC-19. A provider-returned response model remains response metadata and does not select another price.

### Settings contract

- CFG-01: A model can omit `pricing`. Omission makes estimated cost unavailable for that model.
- CFG-02: A present `pricing` mapping requires finite nonnegative `input`, `output`, `cacheRead`, and `cacheWrite` rates in USD per 1,000,000 tokens.
- CFG-03: Each pricing tier requires a positive `inputTokensAbove` threshold and all four finite nonnegative rates. Thresholds must be strictly increasing.
- CFG-04: Zero rates represent known free usage. They do not represent unavailable pricing.
- CFG-05: Unknown pricing fields, non-finite values, negative values, duplicate thresholds, and unordered thresholds fail settings loading.

Example structure. Values are configuration examples, not a provider price catalogue.

```yaml
providers:
  openai-codex:
    type: openai-codex
    api: responses
    models:
      - id: gpt-5.6-luna
        reasoning:
          supported: true
          choices: [off, low, medium, high, xhigh]
          default: high
          wireFormat: openai-responses
        pricing:
          input: 1.25
          output: 10
          cacheRead: 0.125
          cacheWrite: 1.25
          tiers:
            - inputTokensAbove: 272000
              input: 2.50
              output: 15
              cacheRead: 0.25
              cacheWrite: 2.50
```

### Programmatic Control contract

- APC-21: The bidirectional `Open` RPC remains the only Programmatic Control transport operation.
- APC-22: `OpenRequest.command` adds `CreateSession`, `ListSessions`, `ResumeSession`, `SetSessionName`, `GetSessionInfo`, `GetSessionEntries`, and `GetSessionStats`.
- APC-23: Create, resume, and name commands return `SessionInfoResult`. List returns `SessionsResult`. Entry and statistics queries return their typed results.
- APC-24: `SessionInfo` contains ID, user-name presence and value, canonical working directory, storage-path presence and value, creation time, and update time.
- APC-25: `SessionSummary` contains `SessionInfo`, first user text, and total-message count. `SessionsResult` is ordered by APC-08.
- APC-26: `SessionEntry` contains entry ID, timestamp, and exactly one user message, model response, or tool result. User content is an ordered text-or-image union. Model response mapping omits provider context.
- APC-26.1: `UserMessage.text` is removed and its tag and name are reserved. `UserMessage.content` becomes the provider-neutral ordered text-or-image contract. No legacy text projection remains.
- APC-27: `SessionStatistics` exposes the counts and values from ENT-07 through ENT-07.2 and the provider-model cost breakdown from APC-20. Comments define reasoning tokens as a subset of output tokens.
- APC-28: Session replacement during a run returns `REJECTION_CODE_BUSY`. Unknown session ID returns `REJECTION_CODE_NOT_FOUND`. Invalid names return `REJECTION_CODE_INVALID_ARGUMENT`. Corrupt or unsupported files return the new `REJECTION_CODE_SESSION_UNAVAILABLE`. Persistence failures and mutations of a write-unavailable active session return the new `REJECTION_CODE_PERSISTENCE_UNAVAILABLE`.
- APC-28.1: Programmatic Control preserves the evaluation order through active-correlation reuse from PHS-02 DEC-07.3. Create and resume then validate their command arguments, try the operation gate, and access storage. This order makes busy win over not found or unavailable storage after valid resume arguments.
- DEC-13: Existing Programmatic Control fields keep their assigned tag numbers. New fields use new tags. No deprecated session contract or compatibility adapter is added.

### UI plugin and standard TUI contract

- APC-29: UI initialization adds active `SessionInfo`. The initial session has no transcript because startup creates a new session.
- APC-30: UI commands add create session, list sessions, resume session by ID, set session name, and get session information.
- EVC-01: `SessionList` carries ordered summaries with session information, first user text, and total-message count for the `/resume` selector.
- EVC-02: `SessionChanged` carries active session information and all client-visible entries. UI user content uses the ordered text-or-image union from APC-26, and UI model content omits provider context. The TUI replaces its process-local transcript instead of appending resumed entries as live events.
- EVC-03: `SessionInformation` carries active information and statistics for `/session`.
- APC-31: The standard TUI handles exact commands `/new`, `/resume`, `/name`, `/name <nonempty name>`, and `/session`. Bare `/name` shows the active name or name-command usage. Text that matches none of these forms produces `CommandSubmit`.
- APC-32: `/resume` opens a TUI-local selector. Up and down change selection, Enter confirms, and Escape cancels. Rows display the user name or first user text, update time, and message count. Display text is normalized to one line and truncated to row width with an ellipsis.
- APC-33: `/new` and resume replace transcript content only after Host confirmation. A rejected operation preserves the active transcript and editor.
- APC-34: PHS-04 adds no session deletion, tree controls, viewport rebuild, attachment editor, or new transcript rendering policy.

### Failure behavior

- FLR-01: Failure to create the session root or project directory fails application startup before a provider request or client initialization.
- FLR-02: Explicit resume of an unknown, malformed, wrong-directory, or unsupported-version session fails and preserves the active session.
- FLR-03: List skips invalid session files and logs one structured warning per skipped file. It does not expose persisted content in logs.
- FLR-04: Append, permission, sync, close, or encoding failure on the active session stops the affected run or naming operation, leaves the active in-memory snapshot unchanged, and marks the active session write-unavailable. A user-entry failure follows STP-01.1.
- FLR-04.1: A write-unavailable session rejects later history appends and naming without another storage attempt. List, information, entries, and statistics remain available. Create or a successful resume replaces the active session and restores writable state.
- FLR-04.2: Load, validation, truncate, sync, or close failure during resume preserves the previous active session and its writable or write-unavailable state. A later resume validates the target file under DEC-10 and DEC-11.
- FLR-05: A model response with unavailable usage is persisted with unavailable usage and cost. Glyph does not substitute zero.
- FLR-06: A model with omitted pricing persists usage and unavailable cost. A configured zero price produces an available zero cost.
- FLR-07: Provider context is persisted and replayed only through provider drivers. It is absent from Programmatic Control responses, UI frames, logs, and safe error messages.
- FLR-08: Two processes writing one session have undefined results under the approved unsupported scenario. Glyph does not claim detection or recovery for concurrent writes.
- CNS-03: Storage warnings and errors use structured `slog` calls with context, operation, session ID when known, and the underlying error. Logs exclude user content, model content, tool arguments and results, provider context, images, and session names.

### Verification strategy

- CHK-01: JSONL tests reopen text, images, provider context, tool data, outcomes, usage, calculated cost, and name with equal domain values. Image and provider-context bytes remain exact. Extension data remains semantically equal after compact JSON serialization.
- CHK-02: Agent Core tests prove that a user append precedes its provider request, a model append precedes tool execution, and a tool-result append precedes another provider request.
- CHK-03: Programmatic Control and standard TUI integration tests reconstruct application composition, resume by session ID, and observe the restored terminal history in the next provider request.
- CHK-04: Failure tests cover busy replacement, unknown ID, invalid name, unsupported version, malformed completed record, interrupted final append, write failure, sync failure, write-unavailable recovery, unavailable usage, and unavailable pricing.

## Overengineering and Overspecification Considerations

- TRD-01: JSONL uses the standard library and one file per session. PHS-04 adds no database, index, daemon, lock, lease, or merge protocol.
- TRD-02: Statistics are derived by scanning session entries. PHS-04 adds no cached aggregate or background index without measured need.
- TRD-03: The format contains only linear order. Parent links, active leaves, labels, branch summaries, and compaction entries remain in their owning stages.
- TRD-04: The TUI adds only the session commands and a minimal selector. The PHS-12.1 through PHS-12.3 rebuild remains separate.
- TRD-05: Pricing follows the four Pi rate buckets and zero or more request-wide tiers. PHS-04 adds no service-tier, currency conversion, invoice reconciliation, or remote pricing catalogue.
- TRD-06: Unsupported format versions fail explicitly. The new project adds no migration or compatibility reader.
- TRD-07: Crash recovery changes only one incomplete final record. Glyph does not silently skip malformed completed records.
- TRD-08: Extension envelopes are round-tripped but have no public producer or client projection until the owning extension-session stage.
- TRD-09: SQLite was rejected because PHS-04 has one local append sequence, no query scale target, and no approved concurrent-writer behavior. JSONL provides the required ordering and inspection with no package change.
- TRD-10: Provider-billed cost was rejected because the supported provider responses do not return invoice amounts. Persisted cost is an estimate calculated from configured Pi-style rates and reported token usage.
- TRD-11: The process-local operation gate is one nonblocking mutex shared by run coordination and session replacement. It closes the accepted-run race without adding interprocess coordination or session-file locking.
- TRD-12: Pi updates session memory before synchronous file append, defers a new file until the first terminal model response, and does not call `fsync`. Glyph rejects that write order because PHS-04 requires storage failure to stop dependent provider and tool work without advancing the active snapshot.

## Open Questions

None.

## References

- REF-01: `docs/specs/features/initial/delivery-plan/04-persistent-linear-sessions.md` - owning ticket and acceptance criteria.
- REF-02: `docs/specs/features/initial/prd.md` - product session and Programmatic Control requirements.
- REF-03: `docs/terms.md` - domain terminology.
- REF-04: `docs/specs/features/initial/phs-03-providers-models-runtime-selection_solution.md` - provider-context and Agent Core boundaries.
- REF-05: `host/internal/domain/agent/model.go` - current provider-neutral history entries and tool results.
- REF-06: `host/internal/domain/model/model.go` - current messages, responses, usage, tool calls, and provider context.
- REF-07: `host/internal/usecase/agent/run/service.go` - current in-memory history and terminal append order.
- REF-08: `host/internal/usecase/host/programmatic/service.go` - current Programmatic Control use case.
- REF-09: `host/internal/usecase/host/ui/session.go` - current UI Host session loop.
- REF-10: `host/internal/infra/persistence/paths.go` - current Glyph data paths and permissions.
- REF-11: `api/programmatic/v1/programmatic.proto` - Programmatic Control transport contract.
- REF-12: `api/plugins/ui/v1/ui.proto` - UI plugin transport contract.
- REF-13: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/session-format.md` - Pi JSONL session reference.
- REF-14: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/docs/rpc.md` - Pi session statistics reference.
- REF-15: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/dist/core/session-manager.js` - Pi append and discovery behavior used for feature comparison.
- REF-16: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/types.d.ts` - Pi pricing and context model used for feature comparison.
