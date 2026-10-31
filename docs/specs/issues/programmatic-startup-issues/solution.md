# Technical solution: startup issues in Programmatic Control

## Problem statement

See [problem statement](problem.md).

## Proposed solution

Programmatic Control gets a new connection event for one startup issue. The event carries the affected extension IDs, the catalog path, and the complete error text. The existing `ExtensionIssue` event describes one failed handler with a handler ID and a code, so it cannot carry a startup issue.

The Programmatic Control composition keeps the startup report that `startup.Service.Load` returns. When the client opens its stream, the Programmatic output adapter sends one event for each issue, in report order, before any other event on the stream. Host loads extensions before any client can connect, so the report is complete at that moment. One `glyph` process accepts at most one Programmatic Control stream (`Service.open` in `host/internal/controller/programmatic/service.go`), so each issue reaches the client once.

Example: extensions `a` and `b` both register `edit`. Right after the client opens the stream, it receives one startup-issue event with extension IDs `a` and `b` and the text `tool name "edit" conflicts`.

## Verification

An integration test in `host/internal/app` starts Host in Programmatic Control mode with two extensions from `testdata/external-plugins` that register the same tool name. The client receives the startup-issue event with both extension IDs and the complete text before the result of its first request.

## Overengineering and overspecification considerations

The solution adds one event and no query command, retention policy, or repeated delivery, because a process has one stream.

## Open questions

None.
