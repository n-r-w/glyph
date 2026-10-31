# Problem statement

## Context

At startup, Host discovers extensions, starts their processes, and validates their registrations. Each failure that affects only some extensions becomes a startup issue with the affected extension IDs, the catalog path, and the complete error text. Host then continues without the affected extensions.

## Observed problem

In Programmatic Control mode, nobody learns the text of a startup issue. `runProgrammaticWithPaths` in `host/internal/app/programmatic.go` calls `startup.Service.Load` and discards the returned report. `startup.Service.Load` logs only the number of issues (`logReport`). A Programmatic Control client therefore sees an agent without some tools or handlers and cannot learn why.

The other modes report every issue. One-shot `glyph run` calls `startup.Service.Start`, and the headless renderer writes each issue to stderr. The UI mode calls the same method, and the UI runtime passes the complete report to the UI plugin at initialization.

## Affected audience

Programmatic Control clients and their users, whenever an extension fails to start, fails validation, or conflicts with another extension. Examples: a directory that cannot be read, a process that exits during registration, an invalid input schema, two extensions that register the same tool name.

## Evidence

- `host/internal/app/programmatic.go`, `runProgrammaticWithPaths`: the result of `startupService.Load` is discarded.
- `host/internal/usecase/host/startup/service.go`, `logReport`: the log entry holds `issue_count` and no issue text.
- `host/internal/usecase/host/startup/service.go`, `Service.Start`: the other modes pass each issue to `Reporter.ReportIssue` and the report to `Reporter.ReportSummary`.

## Impact

HIGH. The user of a Programmatic Control client sees missing tools and handlers without a reason. To find the cause, the user must run the same extensions in another mode. [PHS-09](../../features/initial/phases/09-tool-middleware-run-control/prd.md) adds rejected tool replacements as another kind of startup issue, which these clients would also miss.

## Desired state

A Programmatic Control client receives the complete text of each startup issue without knowing in advance that issues exist.

## Problem boundary

The issue concerns startup issues in Programmatic Control mode. Issues that extension handlers report during operation already reach the client as `ExtensionIssue` connection events.

## Open questions

None.
