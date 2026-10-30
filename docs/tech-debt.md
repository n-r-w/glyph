# Technical debt

## TD-01: Extension runtime stores a context

- Status: Open.
- Location: `host/internal/infra/plugins/extension/runtime/runtime.go`, `Runtime.context`.
- Problem: `Runtime` stores `context.Context` in a struct field, contrary to the Go guideline to pass contexts explicitly to operations that need them.
- Verification: The field declaration was confirmed during the UNT-01 review and rechecked when recording this item. Context lifetime behavior and runtime consequences remain uninvestigated.
- Scope: Recorded outside UNT-01; no implementation correction has been made.
