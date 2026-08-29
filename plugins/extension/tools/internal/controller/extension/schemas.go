package extension

const (
	standardToolCount = 7
	readToolName      = "read"
	writeToolName     = "write"
	editToolName      = "edit"
	grepToolName      = "grep"
	findToolName      = "find"
	listToolName      = "ls"
	bashToolName      = "bash"

	readInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the file to read."},` +
		`"offset":{"type":"integer","minimum":1,"description":"One-based line offset."},` +
		`"limit":{"type":"integer","minimum":1,"description":"Maximum number of lines."}},` +
		`"required":["path"],"additionalProperties":false}`
	writeInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the file to write."},` +
		`"content":{"type":"string","description":"Complete file content."}},` +
		`"required":["path","content"],"additionalProperties":false}`
	editInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string","description":"Path to the file to edit."},` +
		`"edits":{"type":"array","minItems":1,"items":{"type":"object","properties":` +
		`{"oldText":{"type":"string","minLength":1},"newText":{"type":"string"}},` +
		`"required":["oldText","newText"],"additionalProperties":false}}},` +
		`"required":["path","edits"],"additionalProperties":false}`
	grepInputSchemaJSON = `{"type":"object","properties":` +
		`{"pattern":{"type":"string"},"path":{"type":"string"},"glob":{"type":"string"},` +
		`"ignoreCase":{"type":"boolean"},"literal":{"type":"boolean"},"context":{"type":"integer","minimum":0},` +
		`"limit":{"type":"integer","minimum":1}},"required":["pattern"],"additionalProperties":false}`
	findInputSchemaJSON = `{"type":"object","properties":` +
		`{"pattern":{"type":"string"},"path":{"type":"string"},"limit":{"type":"integer","minimum":1}},` +
		`"required":["pattern"],"additionalProperties":false}`
	listInputSchemaJSON = `{"type":"object","properties":` +
		`{"path":{"type":"string"},"limit":{"type":"integer","minimum":1}},"additionalProperties":false}`
	bashInputSchemaJSON = `{"type":"object","properties":` +
		`{"command":{"type":"string","description":"Bash command to execute."},` +
		`"timeout":{"type":"number","exclusiveMinimum":0,"description":"Timeout in seconds; no default timeout."}},` +
		`"required":["command"],"additionalProperties":false}`
)
