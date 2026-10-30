# PHS-08 decisions

Decisions below are inside the [confirmed authority](target-picture.md). They supplement the [technical solution](technical-solution.md) and preserve the ticket's scope.

## D8-01: Context composition follows prepared-context revisions

Host applies instruction/context middleware after existing conversation preparation. Ordinary transient retries reuse the effective result. A committed overflow recovery creates new prepared history and therefore a new original/current composition using frozen registrations, selection and captured owner. A failed handler is terminal and is never retried.

An arbitrary replacement F(H) contains no reusable rule for later compacted history C(H). Reusing the old value would ignore recovery; assigning new history directly would discard middleware effects. Recomposition after successful preparation preserves both requirements. The analytical proposal to require one successful invocation across an entire logical execution was stronger than the confirmed ticket and is not adopted.

## D8-02: Serialized replacement uses a confidential projection

Public serialized-request handlers receive safe JSON containing invocation-local references in place of private provider replay. Retained references restore exact private fragments; removed references discard replay. Credentials, account identity and opaque replay remain private. Invalid references fail before dispatch as extension failure.

The SDK's existing `option.WithMiddleware` supplies the HTTP interception point. Raw-attempt callbacks remain provider-neutral. Moving serializers or HTTP execution into Host and adding a second transport interception layer are unnecessary.

This is a contract-shape selection within delegated authority. Complete authenticated raw-request access was never promised and would violate mandatory architecture confidentiality.

## D8-03: Finalization covers canonical messages and configured results

Finalized-user replacement occurs after input processing and before final modality validation. Core applies model/tool-result replacement before append and uses the same final values for events and subsequent behavior. Configured terminal responses use the same owner without conversation persistence. Ordinary conversation model results are finalized once in Core, not again in model execution.

Role and Host-owned accounting/execution identity are preserved. Provider-private context uses local references in public projections. Failed replacement does not append its candidate, conceal completed tool actions, or recursively invoke the failed chain.

## D8-04: Final input incompatibility has a public category

Use `INPUT_INVALID` for well-formed final user input incompatible with the selected descriptor. Invalid handler actions remain `EXTENSION_FAILED`. Both meanings supplement complete source text. Programmatic/UI mappings and command allowlists must distinguish this asynchronous final-input failure from admission-shape rejection and provider failure.

## D8-05: Fully handled input completes without a synthetic run

Runcontrol completes the accepted client operation successfully and releases its reservation. It invokes no Core execution, user append, provider request or synthetic run lifecycle. Existing client completion and availability paths remain responsible for idle recovery.

## D8-06: All supported callers share applicable middleware

Programmatic/UI submit contracts represent ordered text/image blocks. Standard TUI and one-shot retain their current text UX by constructing one text block. Configured model requests share instruction/context, provider-wire and completed-model transformation while retaining their current absence of automatic conversation preparation. Add no legacy text/content fallback or blanket nested-call bypass.

## D8-07: Public headers exclude private authentication state

Provider mapping retains credential, cookie, proxy authentication and account headers privately. Ordinary extensions can transform public headers and add ordinary custom headers. Protected writes fail. Response observations exclude protected authentication/cookie data and redact known authentication secrets. Protected account-header exclusion applies to structured middleware payloads. Error text retains ordinary account, organization and project identifiers because they are not authentication secrets. Host middleware coordination imports no credential or concrete provider package.

## Terminology check

| Concept | Candidate | Position | Level | Result |
| --- | --- | --- | --- | --- |
| Request-local provider capability retaining selected provider and captured owner | prepared provider binding | No new domain name requested | Implementation detail | Explain the object in the solution; use an implementation identifier in code; no glossary term |
| New history produced by initial preparation or committed overflow | prepared-context revision | Special case of prepared history | Implementation detail | Explain the successful recomposition condition; no glossary term |
| Public placeholder identifying private replay for one invocation | replay reference | No new domain name requested | Implementation detail | Define its preserve/discard behavior at the public contract; no global domain rename |

## Open questions

None.
