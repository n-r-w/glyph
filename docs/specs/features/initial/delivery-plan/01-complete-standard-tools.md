# Ticket: PHS-01 - Complete standard tools

Deliver bounded, production-usable `read`, `write`, `edit`, `grep`, `find`, `ls`, and `bash` behavior instead of only completing the tool-name catalogue.

## Key definitions and abbreviations

- DEF-01: Standard tool output budget. Model-visible text is limited to 50 KiB or 2,000 lines, whichever limit is reached first.
- DEF-02: Supported image. A JPEG, static PNG, GIF, WebP, or BMP file identified from its content rather than its filename.

## Problem Statement

- PRB-01: The bundled tools extension exposes only read, edit, and bash. Read returns complete files, the schema accepts only required strings, and standard-tool output has no shared bound.

## Target Picture

- SOL-01: Deliver bounded, production-usable `read`, `write`, `edit`, `grep`, `find`, `ls`, and `bash` behavior instead of only completing the tool-name catalogue.

## Scenarios

### SCN-01: Primary completion scenario

- Actor: coding-agent user.
- Pre-condition: DEP-01 is met.
- Trigger: the user asks Glyph to inspect and change a project.
- Required behavior: the agent uses bounded partial reads, searches, listings, file mutations, and shell execution to complete the task.
- Example input and expected output: Input: request `read` with `offset=101` and `limit=50` for a 200-line file. Expected output: lines 101 through 150 and continuation offset 151, with no result above the standard output budget.

## Scope

In scope:

- ISP-01: The behavior and artifacts defined by FRQ-01 onward, DLV-01 onward, and ACC-01 onward.

Out of scope:

- OSP-01: No tool middleware, persistent sessions, provider changes, or TUI-specific rendering.

## Dependencies and Preconditions

- DEP-01: [PHS-00](00-prototype-baseline.md) must meet all acceptance criteria.

## Requirements

### Goals

- GOL-01: Deliver bounded, production-usable `read`, `write`, `edit`, `grep`, `find`, `ls`, and `bash` behavior instead of only completing the tool-name catalogue.

### Functional Requirements

- FRQ-01: Upgrade `read` from complete-file-only operation to offset and limit reads for text files, typed image results for DEF-02, and explicit continuation information when a result is partial. A partial text result appends a notice in the same text block that identifies the shown line range, total line count, and next `offset`. The complete text block, including the notice, remains within DEF-01. When the first requested line exceeds the byte limit, `read` returns a bounded text notice identifying the line and byte size and directs the model to use a bounded `bash` command. When the first requested line fits DEF-01 alone but cannot fit with the required continuation notice, `read` returns a bounded text notice with the line, byte size, bounded `bash` command, and the next line `offset`. It does not return a partial line or byte offset.
- FRQ-02: Add `write` with parent-directory creation and upgrade `edit` to apply one or more unique exact replacements as one file mutation. A missing or non-unique source fragment leaves the file unchanged.
- FRQ-03: Add `grep`, `find`, and `ls` with the input and result controls in the Standard tool capability baseline below.
- FRQ-04: Add `bash` timeout input, retain streamed stdout and stderr, terminate the process group on timeout or cancellation, and store complete output in a temporary file when the model-visible result exceeds DEF-01.
- FRQ-05: Replace the prototype string-only schema profile with JSON-compatible tool arguments that support strings, numbers, booleans, null, arrays, nested objects, and optional fields.
- FRQ-06: Apply DEF-01 to every textual standard-tool result while preserving Host schema validation, cancellation, and model-visible operation errors.

The Pi tool implementations in REF-04 through REF-11 provide evidence for the coding-agent capabilities in this baseline. They do not define Glyph API or source compatibility.

#### Standard tool capability baseline

| Tool | Required input and behavior | Partial or large result behavior |
|---|---|---|
| `read` | Required `path`; optional one-based `offset`; optional positive `limit`; text files and DEF-02 images | A partial text result includes the shown line range, total lines, and next `offset` in its text block. A first line that exceeds DEF-01 returns a bounded notice with its line and byte size. Image content uses typed image result content |
| `write` | Required `path` and `content`; creates missing parent directories; replaces complete file content | Confirmation identifies the written path; content is not echoed beyond DEF-01 |
| `edit` | Required `path` and one or more ordered exact replacements; every source fragment must occur exactly once in the pre-mutation content | All replacements commit together; any invalid replacement returns an error and leaves the file unchanged |
| `grep` | Required pattern; optional path, glob, case-insensitive mode, literal mode, context lines, and positive match limit | Returns matching paths and lines, reports the reached match or output limit, and does not return unbounded lines |
| `find` | Required glob pattern; optional root path and positive result limit; returns project-relative paths | Reports the reached result or output limit |
| `ls` | Optional path and positive entry limit; marks directories distinctly | Reports the reached entry or output limit |
| `bash` | Required command; optional positive timeout in seconds with no default timeout; streams stdout and stderr separately | Returns exit code and bounded output; a truncated result identifies the temporary file containing complete combined output |

### Non-Functional Requirements

- NFQ-01: Focused behavioral tests must demonstrate RED and GREEN for this ticket, followed by passing `task lint` and `task test`.
- NFQ-02: Agent Core must remain independent of protobuf, gRPC, plugin SDKs, persistence adapters, and TUI packages. This requirement applies to changes that cross those boundaries.

### Deliverables

- DLV-01: Bundled tools extension containing all seven standard tools with the capability baseline above.
- DLV-02: Shared standard-tool truncation metadata and temporary-output handling required by DEF-01.
- DLV-03: Public tool contract and Host runtime supporting typed text and image results and the required argument shapes.

### Acceptance Criteria

- ACC-01: Through the standard TUI, the agent can locate files, read them, update an existing file, create a file, run a command, and report the result.
- ACC-02: Reading with `offset` and `limit` returns exactly the requested available lines and identifies the next offset when more content remains.
- ACC-03: A multi-replacement edit changes the file once when every source fragment is unique and leaves the file byte-for-byte unchanged when any source fragment is missing or duplicated.
- ACC-04: `grep`, `find`, and `ls` apply every filter and limit listed in the capability baseline and report which limit truncated the result.
- ACC-05: No textual standard-tool result exceeds 50 KiB or 2,000 lines. A truncated `bash` result identifies a readable temporary file containing its complete output.
- ACC-06: `bash` timeout and cancellation terminate the command process group and return the distinct timeout or cancellation outcome.
- ACC-07: Reading any DEF-02 image returns typed image content without converting binary data to text.
- ACC-08: The same coding task completes through headless execution.
- ACC-09: Invalid arguments do not open an extension execution and produce a model-visible error.

## Overengineering and Overspecification Considerations

The ticket introduces only the public behavior needed by SCN-01 and the listed functional requirements. OSP-01 remains outside the ticket. New public contracts require a working producer and consumer in this ticket.

## Constraints and Risks

- RSK-01: Provider schema and image capabilities differ. Keep the Host tool model provider-neutral and let each provider adapter reject an unsupported schema or image before dispatch.
- RSK-02: Reading or capturing complete files and command output can exhaust memory before result truncation. Implement bounded streaming or bounded accumulation at the filesystem and process adapters rather than truncating only after full buffering.
- RSK-03: Concurrent `write` and `edit` calls can lose updates after parallel tool batches are introduced in PHS-09. Route each complete read-modify-write operation through one absolute-path mutation queue before PHS-09 enables parallel execution.

## Assumptions

None.

## Open Questions

None.

## Technical Supplement

No additional technical design is selected by this ticket. Contract shapes and package placement require a phase-specific technical solution before implementation when the functional requirements change a public process boundary.

## References

- REF-01: [target product requirements](../prd.md) - target product requirements.
- REF-02: [ticket order and ownership](index.md) - ticket order and ownership.
- REF-03: [prototype tool process contract](../../../../../api/plugins/extension/v1/tool.proto) - prototype tool process contract.
- REF-04: [`read.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/read.ts) - reference partial text and image reads.
- REF-05: [`write.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/write.ts) - reference complete-file writes and parent-directory creation.
- REF-06: [`edit.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/edit.ts) - reference ordered multi-replacement behavior.
- REF-07: [`grep.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/grep.ts) - reference search filters, context, and limits.
- REF-08: [`find.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/find.ts) - reference file discovery and limits.
- REF-09: [`ls.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/ls.ts) - reference directory listing and limits.
- REF-10: [`bash.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/bash.ts) - reference timeout, streaming, cancellation, and complete-output handling.
- REF-11: [`truncate.ts`](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/src/core/tools/truncate.ts) - reference 50 KiB and 2,000-line output budgets.
