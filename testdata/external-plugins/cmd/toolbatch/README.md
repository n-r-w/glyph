# Signal-driven tool batch fixture

This command uses only the public Extension Contract and SDK. It declares `concurrent` with `ToolDescriptor.concurrent=true` and leaves the declaration absent on `serial`.

Each call accepts `url` and `id` string arguments. The tool sends a context-bound HTTP GET to `{url}/{id}` and waits for the response. The test server signals each arrival and holds the response until the test releases that call. The fixture returns the response body as the tool result.

`host/internal/app/tool_batch_integration_test.go`, `TestPublicConcurrentToolBatch`, drives the fixture through Programmatic Control and observes the next provider request and session history. The test releases the second concurrent call before the first and holds both serial calls before allowing the final concurrent call. See [D5](../../../../docs/specs/features/initial/phases/09-tool-middleware-run-control/solution.md#d5-concurrent-tool-batches) for the execution rules.
