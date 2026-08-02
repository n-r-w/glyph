# Research: Runtime-Loadable Go Extensions

- PRB-01: Can a Go application load third-party extensions that users install, enable, disable, and update without rebuilding the host on macOS and Linux, while preserving a credible path to Windows?

## Scope

- ISP-01: Compare native Go plugins, Go shared-library modes, separate extension executables, interpreted Go, and WebAssembly.
- ISP-02: Evaluate host rebuild requirements, Go authoring, operating-system support, lifecycle control, compatibility, failure isolation, rich host integration, and project maintenance.
- OSP-01: Package registries, download workflows, permission systems, sandboxing, and detailed extension APIs are outside this research.

## Executive Summary

- FND-01: Runtime installation without rebuilding the Go host is feasible.
- FND-02: The standard Go `plugin` package is unsuitable for a portable public extension system because it lacks Windows support, cannot unload plugins, and tightly couples host and extension builds.
- FND-03: Separate extension executables communicating through a versioned local contract provide the strongest fit for rich Go extensions. They support ordinary compiled Go, independent releases, process termination, replacement, and crash isolation.
- FND-04: HashiCorp `go-plugin` with gRPC is the strongest established implementation of the separate-process approach.
- FND-05: WebAssembly through wazero is portable and actively maintained, but its restricted boundary makes direct Go interfaces, live object graphs, and rich terminal UI integration difficult.
- FND-06: Yaegi supports interpreted Go source, but dependency resolution, language compatibility, performance, and resource cleanup limit its suitability for a public extension ecosystem.

## Evidence

### Native Go Loading

- OBS-01: The Go `plugin` package supports Linux, FreeBSD, and macOS. Windows is not supported. Loaded plugins are initialized once and cannot be closed. [REF-01]
- OBS-02: Go warns that native plugins can crash unless the host and every plugin use the same toolchain, build configuration, and common dependency source. The documentation states that host and plugins generally must be built together. [REF-01]
- OBS-03: The Windows support request and plugin-closing request remain open in the Go issue tracker. [REF-03] [REF-04]
- OBS-04: `-buildmode=c-shared` can emit libraries for macOS, Linux, and Windows, but Go does not define it as a Go-host plugin system and does not support unloading Go shared libraries. [REF-02] [REF-05]
- OBS-05: `-buildmode=shared`, `-linkshared`, and `c-archive` require link-time integration and do not provide runtime extension discovery or lifecycle management. [REF-02]

### Separate Extension Executables

- OBS-06: HashiCorp `go-plugin` starts each extension as a subprocess and communicates through local `net/rpc` or gRPC connections. [REF-06]
- OBS-07: `go-plugin` provides process lifecycle management, protocol-version negotiation, bidirectional communication, gRPC streaming, logging, standard stream forwarding, and terminal input inheritance. [REF-06] [REF-07]
- OBS-08: An extension panic occurs in another process and does not directly panic the host. Process separation does not restrict filesystem, network, processor, or memory use. [REF-06]
- OBS-09: Compatible extension executables can be installed and replaced without recompiling the host. The host remains responsible for discovery, installation metadata, enable state, updates, and user-visible errors. [REF-06] [REF-07]
- OBS-10: `go-plugin` v1.8.0 was released on April 29, 2026. The release added a gRPC streaming example and lifecycle fixes. [REF-08]
- OBS-11: `go-plugin` contains Windows-specific process handling and uses loopback TCP on Windows. Its current continuous-integration workflow runs only on Ubuntu, so Windows lifecycle behavior lacks upstream automated validation. [REF-09] [REF-10]
- OBS-12: A custom protocol over framed JSON-RPC is portable, but JSON-RPC does not define process lifecycle, message framing, streaming, cancellation, backpressure, or compatibility negotiation. [REF-11]

### Interpreted Go

- OBS-13: Yaegi evaluates Go source inside a Go host and can expose precompiled host symbols to interpreted code. [REF-12]
- OBS-14: Yaegi does not support Go modules, C calls, assembly, compiler directives, linker directives, or file embedding. Interfaces crossing from compiled code require precompiled wrappers. [REF-12]
- OBS-15: Yaegi documents lower performance for computation-intensive interpreted code and has no documented operation for deterministic interpreter shutdown and resource cleanup. [REF-12] [REF-13]
- OBS-16: Yaegi's latest published release is v0.16.1 from April 3, 2024. Repository commits continued through February 9, 2026. [REF-14] [REF-15]

### WebAssembly

- OBS-17: Go 1.24 and later can compile standard Go source into callable WASI reactor modules using `GOOS=wasip1`, `GOARCH=wasm`, and WebAssembly exports. [REF-16]
- OBS-18: The standard Go WebAssembly boundary supports a restricted set of scalar, pointer, and string types. Go interfaces, maps, slices, channels, and live host objects do not cross the boundary directly. [REF-16] [REF-17]
- OBS-19: wazero is a pure-Go WebAssembly runtime without CGO. It tests Linux, macOS, and Windows and supports explicit module closing. [REF-18] [REF-19]
- OBS-20: wazero v1.12.0 was released on May 29, 2026, and repository updates continued through July 2026. [REF-20]
- OBS-21: WebAssembly extensions require explicit host functions or WASI capabilities for files, processes, networking, terminal access, models, sessions, and other host behavior. [REF-18] [REF-19]
- OBS-22: A WebAssembly artifact is independent of the host operating system and CPU architecture only while every host supplies the same imports, WASI behavior, and extension contract version. [REF-18]

## Analysis

- INT-01: Runtime extension installation does not require native in-process Go loading.
- INT-02: A separate-process boundary best supports broad Glyph-style capabilities such as tools, events, sessions, model access, streaming, and terminal UI contracts.
- INT-03: Separate executables use normal compiled Go and couple compatibility to an explicit extension contract instead of the Go toolchain and dependency graph.
- INT-04: Stopping and replacing an extension process provides clear disable and update semantics. Restarting the host remains the simplest activation policy, but the process model does not require a host rebuild.
- INT-05: Process separation provides a crash boundary, not a security sandbox.
- INT-06: WebAssembly is a strong option for bounded tools and transformations. It is not a proven universal mechanism for rich terminal UI or direct access to Go session and model objects.
- INT-07: Yaegi provides deeper Go-native interaction than WebAssembly, but its package and lifecycle restrictions create substantial compatibility and maintenance risks.
- INT-08: Native Go plugins fail the combined portability, independent distribution, lifecycle, and failure-isolation criteria.

## Options and Trade-offs

| ID | Approach | Strengths | Limitations | Assessment |
|---|---|---|---|---|
| OPT-01 | HashiCorp `go-plugin` with gRPC | Compiled Go, version negotiation, bidirectional streaming, process lifecycle, crash isolation | One process per extension executable; RPC contracts and package management remain host responsibilities; upstream CI is Ubuntu-only | Best established fit |
| OPT-02 | Custom framed JSON-RPC executable protocol | Small dependency surface, inspectable messages, portable process model | Host must implement framing, concurrency, cancellation, streaming, backpressure, compatibility, diagnostics, and lifecycle | Proportionate only for a small contract |
| OPT-03 | wazero with standard Go WebAssembly | Portable artifacts, explicit module lifecycle, active maintenance, no CGO | Restricted ABI, explicit host functions, difficult rich UI and live-object integration | Strong for bounded capabilities |
| OPT-04 | Yaegi source extensions | Familiar Go source, direct access to exposed Go values | No Go modules, precompiled wrappers, interpreted performance, shared-process failures, uncertain cleanup | High compatibility risk |
| OPT-05 | Standard Go `plugin` | Direct in-process Go calls | No Windows, no unload, exact build coupling, no crash isolation | Reject for Glyph's goals |
| OPT-06 | Other native shared-library modes | Official Go build outputs | No supported runtime extension lifecycle; link-time or custom C boundary | Reject as primary mechanism |

## Recommendation

- REC-01: Treat installable Go extensions without rebuilding the host as feasible.
- REC-02: Use independently built extension executables and a versioned local extension contract as the leading technical direction.
- REC-03: Evaluate HashiCorp `go-plugin` with gRPC before creating a custom subprocess protocol.
- REC-04: Keep the product contract independent of the selected transport library.
- REC-05: Do not use the standard Go `plugin` package or other native shared-library modes as the primary extension mechanism.
- REC-06: Keep WebAssembly as an option for bounded extension categories rather than the universal extension mechanism.
- REC-07: This research does not validate replacing one running extension in place. A full environment reload may stop the previous extension runtime and start a new runtime while preserving the session.
- REC-08: Do not promise automatic recovery, unrestricted terminal ownership, native-Go-equivalent WebAssembly or interpreted performance, or Windows support without dedicated validation.

## Open Questions

None.

## References

- REF-01: https://pkg.go.dev/plugin — official platform support, inability to close plugins, build-coupling warnings, and IPC recommendation. Accessed August 2, 2026.
- REF-02: https://go.dev/cmd/go/#hdr-Build_modes — official Go build-mode behavior. Accessed August 2, 2026.
- REF-03: https://github.com/golang/go/issues/19282 — Windows support request for the Go `plugin` package. Accessed August 2, 2026.
- REF-04: https://github.com/golang/go/issues/20461 — request to close loaded Go plugins. Accessed August 2, 2026.
- REF-05: https://github.com/golang/go/issues/11100 — request to unload Go shared libraries. Accessed August 2, 2026.
- REF-06: https://github.com/hashicorp/go-plugin/blob/main/README.md — subprocess architecture, installation model, protocol versioning, streaming, logging, and terminal behavior. Accessed August 2, 2026.
- REF-07: https://pkg.go.dev/github.com/hashicorp/go-plugin — process lifecycle, version negotiation, and protocol APIs. Accessed August 2, 2026.
- REF-08: https://github.com/hashicorp/go-plugin/releases/tag/v1.8.0 — current release evidence. Accessed August 2, 2026.
- REF-09: https://github.com/hashicorp/go-plugin/blob/main/.github/workflows/test.yaml — current continuous-integration matrix. Accessed August 2, 2026.
- REF-10: https://github.com/hashicorp/go-plugin/blob/main/server.go — Windows loopback transport and Unix-socket behavior. Accessed August 2, 2026.
- REF-11: https://www.jsonrpc.org/specification — JSON-RPC 2.0 message model and transport independence. Accessed August 2, 2026.
- REF-12: https://raw.githubusercontent.com/traefik/yaegi/master/README.md — Yaegi features, dynamic extension example, and limitations. Accessed August 2, 2026.
- REF-13: https://pkg.go.dev/github.com/traefik/yaegi/interp — Yaegi interpreter API and lifecycle surface. Accessed August 2, 2026.
- REF-14: https://api.github.com/repos/traefik/yaegi/releases/latest — Yaegi release evidence. Accessed August 2, 2026.
- REF-15: https://api.github.com/repos/traefik/yaegi/commits?per_page=5 — recent Yaegi repository activity. Accessed August 2, 2026.
- REF-16: https://go.dev/blog/wasmexport — standard Go WebAssembly exports, WASI reactors, concurrency, and boundary types. Accessed August 2, 2026.
- REF-17: https://pkg.go.dev/cmd/compile#hdr-WebAssembly_Directives — WebAssembly import and export type mappings. Accessed August 2, 2026.
- REF-18: https://raw.githubusercontent.com/tetratelabs/wazero/main/README.md — wazero runtime, support policy, tested platforms, and maintenance claims. Accessed August 2, 2026.
- REF-19: https://pkg.go.dev/github.com/tetratelabs/wazero/api — module isolation, host functions, memory boundary, and lifecycle interfaces. Accessed August 2, 2026.
- REF-20: https://api.github.com/repos/tetratelabs/wazero/releases/latest — wazero release evidence. Accessed August 2, 2026.
