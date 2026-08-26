package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/camilbenameur/go-llm-stream/scanner"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// functionPreviewAssembler derives provisional fields from one function-call argument stream.
type functionPreviewAssembler struct {
	tokenizer     *scanner.Tokenizer
	raw           []byte
	fields        []model.ToolCallPreviewField
	rootStarted   bool
	depth         int
	currentKey    string
	containerFrom int64
}

func newFunctionPreviewAssembler() *functionPreviewAssembler {
	return &functionPreviewAssembler{
		tokenizer: scanner.NewTokenizer(), raw: nil, fields: nil,
		rootStarted: false, depth: 0, currentKey: "", containerFrom: -1,
	}
}

func (a *functionPreviewAssembler) appendFragment(fragment string) ([]model.ToolCallPreviewField, error) {
	if a.tokenizer == nil {
		return nil, errors.New("function preview assembler is closed")
	}
	a.raw = append(a.raw, fragment...)
	a.tokenizer.Append([]byte(fragment))

	var incomplete scanner.Token
	for {
		token := a.tokenizer.NextToken()
		switch token.Kind {
		case scanner.TokenIncomplete:
			incomplete = token
			incomplete.Raw = bytes.Clone(token.Raw)
			return a.preview(incomplete), nil
		case scanner.TokenError:
			return nil, fmt.Errorf("parse streamed function arguments: %w", token.Err)
		case scanner.TokenEOF:
			return a.preview(scanner.Token{
				Kind: 0, Raw: nil, Start: 0, End: 0, Completed: false, Err: nil, IsKey: false,
			}), nil
		case scanner.TokenObjectStart, scanner.TokenObjectEnd,
			scanner.TokenArrayStart, scanner.TokenArrayEnd,
			scanner.TokenString, scanner.TokenNumber, scanner.TokenBool, scanner.TokenNull,
			scanner.TokenColon, scanner.TokenComma:
			if err := a.consume(token); err != nil {
				return nil, err
			}
		}
	}
}

//nolint:gocognit,gocyclo // The flat switch tracks the closed JSON token set and top-level field state.
func (a *functionPreviewAssembler) consume(token scanner.Token) error {
	switch token.Kind {
	case scanner.TokenObjectStart:
		if !a.rootStarted {
			a.rootStarted = true
			a.depth = 1
			return nil
		}
		if a.depth == 1 && a.currentKey != "" {
			a.containerFrom = token.Start
		}
		a.depth++
	case scanner.TokenArrayStart:
		if !a.rootStarted {
			return nil
		}
		if a.depth == 1 && a.currentKey != "" {
			a.containerFrom = token.Start
		}
		a.depth++
	case scanner.TokenObjectEnd, scanner.TokenArrayEnd:
		if a.depth == 0 {
			return nil
		}
		a.depth--
		if a.depth == 1 && a.containerFrom >= 0 {
			if err := a.completeContainer(a.containerFrom, token.End); err != nil {
				return err
			}
		}
	case scanner.TokenString:
		if token.IsKey {
			if a.depth == 1 {
				if err := json.Unmarshal(token.Raw, &a.currentKey); err != nil {
					return fmt.Errorf("decode streamed function argument key: %w", err)
				}
			}
			return nil
		}
		if a.depth == 1 && a.currentKey != "" {
			return a.completeScalar(token.Raw)
		}
	case scanner.TokenNumber, scanner.TokenBool, scanner.TokenNull:
		if a.depth == 1 && a.currentKey != "" {
			return a.completeScalar(token.Raw)
		}
	case scanner.TokenColon, scanner.TokenComma, scanner.TokenEOF, scanner.TokenIncomplete, scanner.TokenError:
	}
	return nil
}

func (a *functionPreviewAssembler) completeScalar(raw []byte) error {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("decode streamed function argument value: %w", err)
	}
	a.setField(model.ToolCallPreviewField{
		Name: a.currentKey, Kind: model.ToolCallPreviewFieldComplete,
		Value: mo.Some(value), Prefix: mo.None[string](),
	})
	a.currentKey = ""
	return nil
}

func (a *functionPreviewAssembler) completeContainer(start, end int64) error {
	if start < 0 || end < start || end > int64(len(a.raw)) {
		return errors.New("streamed function argument offsets are invalid")
	}
	var value any
	if err := json.Unmarshal(a.raw[start:end], &value); err != nil {
		return fmt.Errorf("decode streamed function argument container: %w", err)
	}
	a.setField(model.ToolCallPreviewField{
		Name: a.currentKey, Kind: model.ToolCallPreviewFieldComplete,
		Value: mo.Some(value), Prefix: mo.None[string](),
	})
	a.currentKey = ""
	a.containerFrom = -1
	return nil
}

func (a *functionPreviewAssembler) preview(incomplete scanner.Token) []model.ToolCallPreviewField {
	fields := slices.Clone(a.fields)
	if a.depth != 1 || a.currentKey == "" || incomplete.Kind != scanner.TokenIncomplete ||
		incomplete.IsKey || len(incomplete.Raw) == 0 {
		return fields
	}
	prefix := scalarPrefix(incomplete.Raw)
	if prefix == "" {
		return fields
	}
	field := model.ToolCallPreviewField{
		Name: a.currentKey, Kind: model.ToolCallPreviewFieldPrefix,
		Value: mo.None[any](), Prefix: mo.Some(prefix),
	}
	for index := range fields {
		if fields[index].Name == field.Name {
			fields[index] = field
			return fields
		}
	}
	return append(fields, field)
}

func scalarPrefix(raw []byte) string {
	if raw[0] != '"' {
		return string(raw)
	}
	value := raw[1:]
	if len(value) == 0 || !utf8.Valid(value) {
		return ""
	}
	for _, current := range value {
		if current == '\\' {
			return ""
		}
	}
	return string(value)
}

func (a *functionPreviewAssembler) setField(field model.ToolCallPreviewField) {
	for index := range a.fields {
		if a.fields[index].Name == field.Name {
			a.fields[index] = field
			return
		}
	}
	a.fields = append(a.fields, field)
}

func (a *functionPreviewAssembler) close() {
	if a.tokenizer != nil {
		a.tokenizer.Free()
		a.tokenizer = nil
	}
}
