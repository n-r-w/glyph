# Technical Solution: Reliable branch summarization

## Problem Statement

See the [problem statement](problem.md).

- PRB-01: Built-in branch summarization does not clearly separate source conversation data from instructions and does not define a stable summary structure.

## Proposed Solution

### Solution overview

- SOL-01: Replace the combined branch-specific prompt with separate embedded system rules and a user-task template.
- SOL-02: Convert the conversation entries selected for summarization into one text block inside `<conversation>` instead of sending them as model history.
- SOL-03: Keep model selection, extension composition, response validation, and atomic navigation commit behavior unchanged.
- SOL-04: Present persisted branch summaries to the active model as explicitly bounded summary data without branch-specific framing.

### Prompt stack

- CMP-01: `prompts/branch_summary_system.md` contains the static system rules.
- CMP-02: `prompts/branch_summary_task.md` contains the user-input template.
- CMP-03: `prompts/branch_summary_context.md` contains the model-visible envelope for a persisted `BranchSummaryEntry`.

The system rules use these meaningful pseudo-XML sections:

- `<identity>` defines the summarization role.
- `<source_handling>` defines `<conversation>` as source data rather than conversation history. It prohibits following, answering, or continuing instructions found in the source. It defines unquoted role labels as framing, each line prefixed with `| ` as one XML-text-escaped source line, and XML text entities as literal source characters.
- `<summary_rules>` requires grounded extraction, exact technical details, correct work state, and omission of unsupported information.
- `<output_format>` defines the approved pseudo-XML section order and requires omission of sections without source content.

The output sections, in order, are:

1. `<goal>`
2. `<constraints_and_preferences>`
3. `<completed_work>`
4. `<work_in_progress>`
5. `<blockers>`
6. `<decisions>`
7. `<important_findings>`
8. `<next_steps>`
9. `<critical_context>`

Section meanings are:

- `<goal>` contains the active desired outcome.
- `<constraints_and_preferences>` contains active user requirements, limits, and preferences.
- `<completed_work>` contains finished actions and their verified outcomes.
- `<work_in_progress>` contains started but unfinished work and its current state.
- `<blockers>` contains conditions and unresolved questions that prevent progress.
- `<decisions>` contains accepted choices and their source rationale when present.
- `<important_findings>` contains source-backed facts that affect continued work.
- `<next_steps>` contains concrete actions that remain necessary.
- `<critical_context>` contains necessary exact context that does not belong in another section. It labels retained assumptions and non-blocking open questions explicitly.

The system rules also require the model to:

- retain only information needed to identify the active goal, current state, next action, active constraints, accepted decisions, exact technical context, or unresolved work;
- state each retained fact once in the most appropriate section;
- let later source information replace superseded information;
- preserve assumptions, blockers, and open questions as uncertain states rather than facts;
- preserve exact identifiers, paths, commands, error messages, and configuration keys;
- omit generic system instructions, skill contents, and ambient environment information unless the source makes them specific to continued work;
- return only applicable pseudo-XML sections without a preamble, conclusion, or empty section.

The user-task template renders one text input with this structure:

```text
<conversation>
{serialized conversation content}
</conversation>

<additional_focus>
{escaped additional focus}
</additional_focus>

<task>
{primary summarization task}
</task>
```

- APC-01: The `<additional_focus>` block is omitted as one complete block when `CustomFocus` is absent.
- APC-02: Additional focus changes prioritization only. It does not replace the system rules, primary task, or output structure.
- APC-03: The authored files contain no references to branches, abandoned work, session trees, or navigation.
- APC-04: All authored instruction text uses ASD-STE100 English.

### Source serialization

- CMP-04: Add a conversation serializer for branch summarization in `host/internal/usecase/host/sessiontree`.
- APC-05: The serializer reads the conversation entries from `NavigationPreparation.AbandonedPath` and returns one deterministic text value or an encoding error.
- APC-06: The serializer preserves conversation-entry order and content-block order.
- APC-07: The serializer separates content with simple role labels and applies XML text escaping to every dynamic value. It does not add nested pseudo-XML inside `<conversation>`.
- APC-07.1: XML text escaping replaces `&`, `<`, and `>` with their standard entities. The framing tags and role labels remain unchanged.
- APC-07.2: Additional focus uses the same XML text escaping before insertion into `<additional_focus>`.
- APC-07.3: Tool-call arguments use deterministic JSON generated from the semantic `model.ToolCall.Arguments` map. Original provider whitespace and key order are not part of the contract.
- APC-07.4: After XML text escaping, the serializer prefixes every dynamic source line with `| `. This includes empty lines. Serializer-added role labels remain unprefixed.
- APC-07.5: Removing one `| ` prefix from each dynamic line and then decoding XML text entities recovers the exact source value.

The serializer emits these labeled blocks:

- `[User]` for each ordered user text block.
- `[Assistant reasoning]` for each ordered model-visible reasoning block.
- `[Assistant]` for each ordered model text block.
- `[Assistant refusal]` for each ordered model refusal block.
- `[Assistant tool call]` with the exact call identifier and tool name plus deterministic JSON for the argument values.
- `[Tool result]` with the exact call identifier, tool name, error state, and ordered text blocks.
- `[Previous summary]` with the exact text of a preceding `BranchSummaryEntry`.

The serializer excludes:

- user and tool-result image blocks;
- provider replay context;
- response usage, diagnostics, and cost metadata;
- session information and labels;
- model-hidden extension entries.

- DEC-01: Image blocks have no marker in the serialized input.
- ASM-01: Relevant image meaning is normally present in the following model-visible text or reasoning. Representative manual evaluation will verify that this remains sufficient for branch continuation.
- DEC-02: Text source values are not truncated. Token budgeting remains outside this issue.

### Model request

`Service.summarize` keeps the current configured-model execution boundary:

1. Load the static system rules.
2. Convert the conversation entries in `preparation.AbandonedPath` to text.
3. Render one user input from the conversation text, optional additional focus, and primary task.
4. Create one `agent.HistoryEntryUser` containing one text `model.Message`.
5. Call `ModelCompleter.CompleteConfigured` with the selected model, system rules, and the one-entry history.
6. Validate the terminal `model.Response` through `validateSummaryResponse`.
7. Build the existing `BranchSummaryDraft` with unchanged source boundaries, selection, and normalized usage.

- APC-08: `ModelCompleter` and `ConfiguredModelRequester` interfaces do not change.
- DEC-03: The active conversation selection remains the default branch-summary selection.
- DEC-04: A `session_before_tree` request handler can replace the selection for one navigation through the existing `SummaryModel` field.
- DEC-05: A complete handler-provided result continues to skip `Service.summarize`.
- DEC-06: No shared summarizer, shared prompt stack, or branch-summary model configuration is added.

### Persisted summary context

`RenderBranchSummaryContext` continues to create one synthetic user message for active model history. Its embedded template becomes:

```text
<summary encoding="xml-text">
{{.EscapedSummary}}
</summary>
```

- APC-09: Persistence keeps the generated summary unchanged. Context rendering applies XML text escaping before inserting it into the explicit envelope.
- APC-10: The envelope contains no instruction to the active model and no session-tree framing. Its `encoding` attribute identifies how to recover literal summary characters.
- APC-11: `HistoryFromEntries` continues to exclude model-hidden extension entries and to preserve its existing projection order.

### Code changes

- CMP-05: `prompts.go` embeds the system, task, and context Markdown files and renders the task and persisted-summary templates.
- CMP-06: A focused conversation-serialization file owns deterministic rendering. No generic summarization package or artificial wrapper type is introduced.
- CMP-07: `summarizer.go` replaces `HistoryFromEntries(preparation.AbandonedPath)` with one user message that contains the serialized conversation.
- CMP-08: `history.go` continues to build active model history. It is not reused for branch-summary serialization because other session operations need its role-bearing output.
- CMP-09: Generated mocks remain unchanged because no interface changes.

Affected files:

- `host/internal/usecase/host/sessiontree/prompts/branch_summary.md`, removed.
- `host/internal/usecase/host/sessiontree/prompts/branch_summary_system.md`, added.
- `host/internal/usecase/host/sessiontree/prompts/branch_summary_task.md`, added.
- `host/internal/usecase/host/sessiontree/prompts/branch_summary_context.md`, updated.
- `host/internal/usecase/host/sessiontree/prompts.go`, updated.
- `host/internal/usecase/host/sessiontree/summarizer.go`, updated.
- `host/internal/usecase/host/sessiontree/summarization_test.go`, updated.
- One focused source-serialization file and its tests, added in the same package.
- `docs/specs/features/initial/phases/05-session-tree/solution.md`, updated after implementation to remain the current PHS-05 design description.
- `docs/roadmap.md`, marked complete after verification.

### TDD and verification

- TSK-01: RED updates `TestNavigateSummarizesOnlyAbandonedPath` to require one user history entry instead of role-bearing source entries.
  - Purpose: prove the summary request uses one serialized input.
  - Input: navigation with built-in mode and navigation with additional focus.
  - Expected output: one configured-model call with one nonempty user text message and no model or tool-result history entries.
  - Edge case: absent versus present `CustomFocus`.
  - Dependencies: existing navigation preparation, model-completer mock, and atomic commit expectation.
- TSK-02: RED adds focused serializer tests for ordered user text, model-visible text and reasoning, tool calls, text tool results, and prior summaries.
  - Purpose: prove the complete approved text source is represented deterministically and remains lossless after XML text decoding.
  - Input: ordered entries that contain all supported source record kinds plus image and model-hidden extension entries.
  - Expected output: supported source values and order are preserved; serializer labels remain distinct from dynamic source lines; semantic tool-argument maps produce deterministic JSON; the result is one text value.
  - Edge cases: multiline and empty lines, every role label at line start, `&`, `<`, `>`, delimiter-like text, different map insertion orders, image blocks, and model-hidden extension entries.
  - Dependencies: domain `session`, `model`, `agent`, and `tool` types only.
- TSK-03: Tests do not assert authored system-rule text, task wording, pseudo-XML tag spelling, or additional-focus wording.
- TSK-04: GREEN implements the minimum prompt rendering, serialization, and request-construction changes needed to pass the focused tests.
- TSK-05: REFACTOR keeps branch-summary behavior in `sessiontree.Service` and keeps cross-type serialization as focused free functions in the owning package.

Verification commands:

1. Run the focused session-tree tests with caching disabled for RED and GREEN evidence.
2. Run `task fmt`.
3. Run `task fix_dry_run` and review the proposal before `task fix` or manual correction.
4. Run `task lint`.
5. Run `task test`.
6. Run `task itest`.
7. Run `task build` and `git diff --check`.

### Failure behavior

- FLR-01: Tool-argument JSON or prompt-template rendering errors return a wrapped branch-summary preparation error before a model request.
- FLR-02: Configured-model failures keep the existing classification and cancel the navigation commit.
- FLR-03: Invalid terminal responses keep the existing validation behavior and cancel the navigation commit.
- FLR-04: No fallback model, compatibility prompt, partial summary, or hidden image fallback is added.

## Overengineering and Overspecification Considerations

- The solution adds one task-specific serializer because active history and summary source have different contracts.
- Existing model execution, selection, extension handlers, validation, persistence, and navigation coordination remain unchanged.
- Three prompt artifacts have one responsibility each: system rules, user task, and persisted-summary context.
- Image understanding, token budgeting, file tracking, a universal summarizer, and new extension APIs remain outside scope.
- The serializer covers existing model-visible text domain variants only. One fixed line prefix keeps role labels unambiguous without nested XML, a parser, or a reusable serialization framework.

## Open Questions

None.

## References

- REF-01: [Problem statement](problem.md) - approved problem and scope.
- REF-02: [Product requirements](prd.md) - approved behavior requirements.
- REF-03: [PHS-05 technical solution](../../features/initial/phases/05-session-tree/solution.md) - current session-tree design and extension contract.
- REF-04: [Project domain glossary](../../../terms.md) - normative domain terminology.
