# Signal-driven tool batch fixture

This command uses only the public Extension Contract and SDK. It declares `concurrent` with `ToolDescriptor.concurrent=true` and leaves the declaration absent on `serial`.

Each call accepts `url` and `id` string arguments and an optional boolean `terminate` argument. The tool sends a context-bound HTTP GET to `{url}/{id}` and waits for the response. The test server signals each arrival and holds the response until the test releases that call. On the ordinary path, the fixture returns the response body as the tool result and copies the requested `terminate` flag into it.

When `abort=true`, the fixture requests cancellation through the SDK after the response arrives. After Host accepts the abort request, the fixture sends an HTTP GET to `{url}/ack`, waits for cancellation of the tool context and returns that cancellation error. `host/internal/app/extension_abort_integration_test.go`, `TestPublicExtensionAbort`, covers this path.

`host/internal/app/tool_batch_integration_test.go`, `TestPublicConcurrentToolBatch`, drives the fixture through Programmatic Control and observes the next provider request and session history. The test releases the second concurrent call before the first and holds both serial calls before allowing the final concurrent call. `host/internal/app/terminate_integration_test.go`, `TestPublicTerminateBatch`, uses the same signals to verify batch completion, mixed flags and pending `steer` or `followUp` delivery. See [D5](../../../../docs/specs/features/initial/phases/09-tool-middleware-run-control/solution.md#d5-concurrent-tool-batches) for the execution rules.
