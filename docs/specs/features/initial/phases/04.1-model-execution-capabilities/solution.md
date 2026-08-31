# Technical Solution: PHS-04.1 Model Execution Capabilities

## Problem Statement

- PRB-01: Model settings and `model.Descriptor` do not contain ordered input modalities, context window, or maximum output tokens.
- PRB-02: PHS-06 cannot obtain token limits from the selected provider-neutral descriptor, and PHS-08 cannot validate model input against declared modalities.
- PRB-03: Programmatic Control cannot expose the execution-capability metadata that later Host behavior will apply.
- PRB-04: Codex tool capabilities depend on executable model-name logic, while OpenAI-compatible models receive fixed false values.

## Proposed Solution

### Solution overview

- SOL-01: Add strict `input`, `contextWindow`, and `maxTokens` fields to every settings-defined model.
- SOL-02: Store the validated values in `model.Descriptor` as the provider-neutral runtime contract.
- SOL-03: Preserve the complete descriptor through PHS-04.1 built-in provider composition and Host catalogue queries. Programmatic Control exposes only `input`, `contextWindow`, `maxTokens`, and reasoning.
- SOL-04: Validate the APC-03 and APC-04 invariants at the Host catalogue boundary so PHS-12 extension registrations can use the contract without a second validation model.
- SOL-05: Use required settings-defined `toolCapabilities` as the temporary source for OpenAI Codex and OpenAI-compatible descriptors until PHS-12.
- SOL-06: Keep context budgeting, input validation, image handling, provider probing, and extension-provider registration outside PHS-04.1.

### Provider-neutral model contract

- ENT-01: Add `model.InputModality` with the closed values `text` and `image`.
- ENT-02: Add ordered `Input []InputModality`, `ContextWindow int64`, and `MaxTokens int64` fields to `model.Descriptor`.
- DEC-01: Use `int64` for both limits in settings, the domain descriptor, and Programmatic Control. `model.Usage` uses `int64` for token counts, so later budgeting does not require numeric conversion.
- CNS-01: `model.Descriptor` contains no YAML, protobuf, provider SDK, or provider wire types.

### Settings contract

- APC-01: Every model mapping requires `input`, `contextWindow`, `maxTokens`, and the top-level `toolCapabilities` mapping. The fields have no provider-type or model-identifier defaults.
- APC-01.1: `toolCapabilities` maps `strictJSONSchema`, `grammar.lark`, and `grammar.regex` to `model.ToolCapabilities`. Omitted boolean members inside the required mapping remain false.
- APC-02: The settings YAML decoder reads `input` as strings and converts validated values to `model.InputModality`; settings serialization types do not enter `model.Descriptor`.
- APC-03: Settings validation rejects an empty input list, a list without `text`, an unknown modality, and a duplicate modality.
- APC-04: Settings validation rejects nonpositive `contextWindow` or `maxTokens` and rejects `maxTokens` greater than `contextWindow`.
- APC-05: Successful settings loading preserves modality order, both integer values, and all three tool-capability booleans exactly.

Example:

```yaml
models:
  - id: vision
    input: [text, image]
    contextWindow: 131072
    maxTokens: 16384
    toolCapabilities:
      strictJSONSchema: true
      grammar:
        lark: true
        regex: true
    reasoning:
      supported: false
      choices: [off]
      default: off
```

### Built-in provider composition and PHS-12 transition

- CMP-01: Until PHS-12, `host/internal/app` maps validated settings-defined model metadata into `model.Descriptor` for the OpenAI Codex and OpenAI-compatible providers. The mapping does not infer modalities, limits, or tool capabilities from provider type or model identifier.
- DEC-02: `host/internal/app` copies validated settings-defined `ToolCapabilities` into OpenAI Codex and OpenAI-compatible descriptors. Executable provider code contains no model-identifier branch or model-specific capability table.
- DEC-03: PHS-12 replaces the settings-to-built-in-descriptor composition path with extension provider registration. Each bundled extension publishes complete descriptors from declarative extension-owned model catalogue data. The descriptor type and catalogue validation and copying continue to apply. Programmatic Control continues to expose only its defined projection.
- CNS-02: PHS-04.1 does not add provider extension registration or move provider implementations. PHS-12 owns that work.

### Host catalogue

- APC-06: `providers.New` validates every descriptor's input modalities and limits with the same rules as APC-03 and APC-04. Descriptor validation applies independently of the descriptor source.
- APC-07: `Catalog.Models` and `Catalog.Snapshot` return descriptors whose `Input` slices do not share backing arrays with catalogue state.
- APC-08: Catalogue queries preserve the configured modality order, context window, and maximum output tokens.

### Programmatic Control contract

- ENT-03: Add protobuf `InputModality` with `INPUT_MODALITY_UNSPECIFIED`, `INPUT_MODALITY_TEXT`, and `INPUT_MODALITY_IMAGE`.
- APC-09: Add `repeated InputModality input_modalities = 4`, `int64 context_window = 5`, and `int64 max_tokens = 6` to `ConfiguredModel`.
- APC-10: The Programmatic Control mapper preserves modality order and both integer values. An unknown domain modality returns a mapping error rather than an unspecified protobuf value.
- APC-11: A successful Programmatic Control model catalogue query exposes exact `input`, `contextWindow`, `maxTokens`, and reasoning values for text-only and text-and-image models. It does not expose `toolCapabilities`.
- CNS-03: The UI plugin contract does not change in PHS-04.1 because this phase requires the new public projection only through Programmatic Control.

### Startup failure behavior

- FLR-01: Any APC-03 or APC-04 settings error returns from settings loading before UI plugin startup, Programmatic Control socket creation, provider request construction, or agent-run admission.
- FLR-02: The error identifies the provider and model and the violated field rule. It does not substitute a default or infer a capability.

### Test strategy

Implementation follows RED, GREEN, and REFACTOR for each behavioral slice. Protobuf declaration and generation needed to compile a behavioral test are compile setup, not RED evidence.

| ID | Purpose | Inputs and expected outputs | Edge cases | Dependencies |
|---|---|---|---|---|
| TSK-01 | Prove strict settings loading | Load `input: [text, image]`, `contextWindow: 131072`, and `maxTokens: 16384`; expect exact validated values | Empty input, missing `text`, duplicate and unknown modality, zero and negative limits, output above context | Existing settings test fixtures |
| TSK-02 | Prove catalogue validation and ownership | Construct valid descriptors; expect exact ordered values and no mutation after changing returned `Input` | Invalid modality lists and limits; mutation through `Models` and `Current` results | Existing provider catalogue tests |
| TSK-03 | Prove current application composition | Configure Codex and OpenAI-compatible models with distinct execution and tool capabilities; expect exact descriptors | A known Codex model has all tool capabilities false and an arbitrary Codex model has all values true | Settings validation and provider catalogue construction |
| TSK-04 | Prove Programmatic Control mapping | Map both modality shapes; expect ordered enum values and exact limits | Unknown domain modality returns an error | Generated protobuf contract and controller mapping |
| TSK-05 | Prove the public model query | Request models through the real Programmatic Control transport; expect exact `input`, `contextWindow`, `maxTokens`, and reasoning values for every returned model | Text-only and text-and-image models in one catalogue | Application composition and Unix-socket test fixture |
| TSK-06 | Prove startup ordering | Start UI, RPC, and headless modes with one representative invalid execution-capability setting; expect a settings error and no UI marker, socket, or provider request | One mode-specific external-effect observation per composition path | Exhaustive invalid-value coverage from TSK-01 |

- DEC-04: Each behavioral test must compile and fail on its intended assertion before production behavior is added. A timeout or compile failure is not RED.
- DEC-05: Final verification runs `go fix -diff ./...`, reviews the proposed changes, runs `go fix ./...`, then runs `task lint` and `task test`.

## Overengineering and Overspecification Considerations

- TRD-01: The modality set contains only `text` and `image`, matching the approved Glyph contract and Pi's model input type. Video package previews in Pi are unrelated to model input.
- TRD-02: The solution adds no generic modality registry, capability probing, inferred defaults, built-in JSON model catalogue, or provider-specific capability table.
- TRD-03: Catalogue validation is the only source-independent validation boundary. Settings retain early configuration errors, while the catalogue prepares the same descriptor contract for PHS-12.
- TRD-04: The PHS-12 source transition is documented without implementing extension registration early.
- TRD-05: The project requires no backwards compatibility for settings or protobuf contracts. Existing settings fixtures must add every required descriptor field. Programmatic Control clients must use the `input`, `contextWindow`, and `maxTokens` fields added to its existing reasoning projection.

## Open Questions

None.

## References

- REF-01: `docs/specs/features/initial/phases/04.1-model-execution-capabilities/ticket.md` - owning ticket and acceptance criteria.
- REF-02: `docs/specs/features/initial/prd.md` - product requirements for model execution capabilities and Programmatic Control.
- REF-03: `docs/specs/features/initial/architecture.md` - provider-neutral descriptor ownership and component boundaries.
- REF-04: `docs/terms.md` - definitions of input modality, context window, and maximum output tokens.
- REF-05: `host/internal/domain/model/model.go` - provider-neutral model descriptor and token usage types.
- REF-06: `host/internal/infra/persistence/settings/service.go` - strict settings decoding and validation.
- REF-07: `host/internal/usecase/host/providers/catalog.go` - Host catalogue validation and slice copying.
- REF-08: `host/internal/app/app.go` - PHS-04.1 built-in provider composition path.
- REF-09: `api/programmatic/v1/programmatic.proto` - Programmatic Control model catalogue contract.
- REF-10: `/opt/homebrew/lib/node_modules/@earendil-works/pi-coding-agent/node_modules/@earendil-works/pi-ai/dist/types.d.ts` - Pi model input type used only for feature comparison.
