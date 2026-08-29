# Problem Statement

## Context

The project owner maintains `https://github.com/n-r-w/pi-agent-suite`, a set of extensions that adds agents, context management, and other capabilities to Pi.

Pi is a useful conceptual reference because it combines a minimal agent core with extensive customization. However, `pi-agent-suite` remains an extension package for an external TypeScript platform.

## Problem Statement

The project owner does not have an independently owned agent platform whose agent core and extension contracts can both be evolved in Go.

`pi-agent-suite` can extend Pi only through contracts exposed by Pi. Behavior outside those contracts cannot be implemented solely within `pi-agent-suite`. This prevents the project owner from evolving the complete agent platform in one owned Go codebase.

## Who Is Affected

The problem directly affects the owner and primary developer of `pi-agent-suite`.

The intended audience for an independent platform also includes:
- Go developers who create agents and extensions;
- developers in any programming language who use terminal agents.

## Evidence

- In `pi-agent-suite`, `pi-package/package.json` defines a Pi package with 20 extension entry points.
- The same manifest declares peer dependencies on `@earendil-works/pi-agent-core`, `@earendil-works/pi-ai`, `@earendil-works/pi-coding-agent`, and `@earendil-works/pi-tui`.
- The root `package.json` uses TypeScript and Bun for development and validation.
- The `main-agent-selection`, `run-subagent`, `workflow`, and `custom-compaction` extensions use Pi events, state, sessions, model access, and terminal user interface contracts.
- During product interviews on August 2, 2026, the project owner stated a preference for direct platform ownership and Go instead of TypeScript.

## Impact

- The agent core and `pi-agent-suite` evolve in separate codebases with different ownership boundaries.
- Behavior outside Pi's extension contracts cannot be delivered solely through `pi-agent-suite`.
- Development of the existing solution requires a technology stack the project owner does not want to maintain.
- `pi-agent-suite` cannot serve as an independent foundation for agents outside Pi.

No quantitative estimate of time or resource impact is available. Such measurements are not required to establish this ownership and technology constraint.

## Current State

`pi-agent-suite` provides additional behavior on top of Pi. Pi defines the agent core, base agent loop, model integrations, session behavior, and extension boundaries.

Glyph has an executable prototype whose limits are defined in the [prototype baseline PRD](phases/00-prototype-baseline/baseline-prd.md), but it does not yet implement the complete independently owned product described in the [target PRD](prd.md).

## Desired Outcome

The project owner controls an independent Go agent platform and can evolve its agent core and extension contracts within one codebase.

The platform can serve as a shared foundation for a coding agent and other agents without duplicating the agent core.

## Success Metrics

The problem is solved when:
1. The Glyph repository contains the source required to build and release the agent core and extension contracts without Pi.
2. Glyph production code is implemented in Go.
3. Glyph requirements and public contracts are traceable to Glyph-owned behavior and do not require Pi compatibility.

## Scope

This problem covers:
- ownership of the agent core;
- Go as the platform implementation language;
- reuse of one platform by different agents;
- separation of the minimal agent core from extensible behavior.

## Out of Scope / Non-Goals

This document does not define:
- first-version features;
- built-in tools;
- user interface behavior;
- supported operating systems or model providers;
- the security model;
- the extension loading mechanism;
- architecture or implementation order;
- migration of existing `pi-agent-suite` extensions.

## Constraints

- The platform is implemented in Go.
- Pi is a conceptual reference only.