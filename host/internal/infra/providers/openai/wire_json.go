package openai

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"
)

// encodeWireObject writes validated JSON subtrees without reformatting restored private fragments.
func encodeWireObject(object map[string]jsontext.Value) ([]byte, error) {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	var output bytes.Buffer
	output.WriteByte('{')
	for i, key := range keys {
		if i != 0 {
			output.WriteByte(',')
		}
		encoded, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		output.Write(encoded)
		output.WriteByte(':')
		output.Write(object[key])
	}
	output.WriteByte('}')
	return output.Bytes(), nil
}

// encodeWireArray writes validated JSON subtrees without reformatting restored private fragments.
func encodeWireArray(array []jsontext.Value) []byte {
	var output bytes.Buffer
	output.WriteByte('[')
	for i, value := range array {
		if i != 0 {
			output.WriteByte(',')
		}
		output.Write(value)
	}
	output.WriteByte(']')
	return output.Bytes()
}
