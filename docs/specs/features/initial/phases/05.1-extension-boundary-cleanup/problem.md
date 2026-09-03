# Problem Statement

## Context

PHS-05 and Blocking contract operation processing are complete. PHS-07 is the next feature phase after PHS-05.1 and will add extension context and lifecycle behavior to Glyph Host.

## Observed Problem

The code left by PHS-05 has three structural problems:

1. Agent Core and OpenAI Codex depend on `host/internal/hooks`, although every production composition constructs an empty hook runner. This path performs no configured production behavior but couples Agent Core and the provider to a prototype Host mechanism.
2. `host/internal/usecase/host/extensions.Service` owns both extension runtime management and capability orchestration for tools and session-tree handlers. A change to one capability therefore affects the service that also starts, monitors, and stops every extension runtime.
3. Each public process contract is concentrated in one protobuf source file. `api/plugins/extension/v1/tool.proto` has 540 lines, `api/plugins/ui/v1/ui.proto` has 910 lines, and `api/programmatic/v1/programmatic.proto` has 893 lines.

## Affected Audience

Glyph maintainers who change Agent Core, providers, Host extension behavior, or public process contracts experience this problem.

## Evidence

- `host/internal/app/headless.go`, `host/internal/app/programmatic.go`, and `host/internal/app/ui.go` construct the empty hook runner.
- `host/internal/usecase/agent/run.Service` depends on `host/internal/hooks.ContextRunner`.
- OpenAI Codex code under `host/internal/infra/providers/openai/codex` depends on `host/internal/hooks.ProviderRunner` and contains hook transport mapping.
- `host/internal/usecase/host/extensions.Service` contains extension discovery, startup, registration, availability, monitoring, cancellation, and shutdown. It also contains tool conflict policy, unavailable-tool result construction, session-handler ordering, action validation, and session-tree payload mapping.
- The three protobuf source files contain 2,343 lines in total.
- Tests cover tool and session-tree handler behavior, cancellation, runtime shutdown, public error text, and external extension and UI plugin fixtures.

## Impact

The prototype dependency and combined service do not provide the ownership boundaries required by PHS-07. Adding more capabilities to these structures would increase Agent Core coupling to Host, enlarge `extensions.Service`, and add more declarations to protobuf files that already exceed the project's 500-line guideline.

## Current State

Tests cover tool execution, session-tree handling, operation cancellation, runtime shutdown, public error text, and the external plugin fixtures. No user-facing behavior defect was found. The problem is the structure through which that behavior is implemented and maintained.

## Desired State

Glyph maintainers can add PHS-07 without extending the prototype hook path, adding capability policy to extension runtime management, or further concentrating unrelated declarations in the three public protobuf source files.

## Problem Boundary

The problem includes only the implemented hook path, tools, session-tree handlers, and source organization of the Extension Contract, UI Plugin Contract, and Programmatic Control. It excludes the extension capabilities planned for PHS-07 and later phases.

## Assumptions

None.

## Open Questions

None.
