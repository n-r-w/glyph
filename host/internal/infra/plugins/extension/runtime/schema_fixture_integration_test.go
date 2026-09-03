//go:build integration

package runtime

// validSchemaJSON is the valid tool schema fixture for integration tests.
const validSchemaJSON = `{"type":"object","properties":{"path":{"type":"string",` +
	`"description":"File path."}},"required":["path"],"additionalProperties":false}`
