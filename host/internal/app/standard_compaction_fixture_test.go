//go:build integration

package app

const (
	// standardRetainedEnvironment names the environment variable for the bundled retention target.
	standardRetainedEnvironment = "GLYPH_COMPACTION_RETAINED_TOKENS"
	// standardGeneratorEnvironment names the environment variable for bundled generator registration.
	standardGeneratorEnvironment = "GLYPH_COMPACTION_GENERATOR_ENABLED"
	// standardRequestExecutable identifies the request-only fixture runtime.
	standardRequestExecutable = "reference-requests"
)
