package main

import (
	"encoding/json"

	"fmt"

	"os"

	"path/filepath"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// executeSafeRead validates model JSON before reading the single generated sample file.
func executeSafeRead(workDir string, schema *jsonschema.Schema, rawArguments string) (string, error) {
	var instance any
	if err := json.Unmarshal([]byte(rawArguments), &instance); err != nil {
		return "", fmt.Errorf("decode arguments for schema validation: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return "", fmt.Errorf("validate arguments against read schema: %w", err)
	}

	decoder := json.NewDecoder(strings.NewReader(rawArguments))
	decoder.DisallowUnknownFields()
	var arguments readArguments
	if err := decoder.Decode(&arguments); err != nil {
		return "", fmt.Errorf("decode typed read arguments: %w", err)
	}
	if arguments.Path != sampleFileName {
		return "", fmt.Errorf("read path %q is outside the safe spike contract", arguments.Path)
	}
	content, err := os.ReadFile(filepath.Join(workDir, sampleFileName))
	if err != nil {
		return "", fmt.Errorf("read generated sample file: %w", err)
	}
	return string(content), nil
}
