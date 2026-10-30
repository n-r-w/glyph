package openai

import (
	"bytes"
	"crypto/rand"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strconv"
)

const (
	// replayReferenceKey identifies private-fragment references in safe JSON.
	replayReferenceKey = "$glyph_replay"
	// wireInputField identifies Responses input.
	wireInputField = "input"
	// wireMessagesField identifies Chat history.
	wireMessagesField = "messages"
	// wireDetailsField identifies OpenRouter replay.
	wireDetailsField = "reasoning_details"
	// wireRoleField identifies a Chat message role.
	wireRoleField = "role"
	// wireAssistantRole identifies the replay-owning Chat role.
	wireAssistantRole = "assistant"
	// wireTypeField identifies a Responses input item variant.
	wireTypeField = "type"
	// wireReasoningType identifies Responses private replay.
	wireReasoningType = "reasoning"
)

// wireLocation distinguishes actual replay slots from similarly named ordinary JSON objects.
type wireLocation uint8

const (
	// wireOther contains ordinary public JSON.
	wireOther wireLocation = iota
	// wireRoot contains the serialized request object.
	wireRoot
	// wireInputArray contains Responses input items.
	wireInputArray
	// wireInputItem can contain one private Responses reasoning item.
	wireInputItem
	// wireMessagesArray contains Chat messages.
	wireMessagesArray
	// wireMessage contains one Chat message.
	wireMessage
	// wireDetails contains the private OpenRouter replay collection.
	wireDetails
)

// objectChild identifies replay locations from actual provider-owned parent structure.
func (location wireLocation) objectChild(key string, object map[string]jsontext.Value) wireLocation {
	if location == wireRoot {
		switch key {
		case wireInputField:
			return wireInputArray
		case wireMessagesField:
			return wireMessagesArray
		}
	}
	if location == wireMessage && key == wireDetailsField {
		var role string
		if json.Unmarshal(object[wireRoleField], &role) == nil && role == wireAssistantRole {
			return wireDetails
		}
	}
	return wireOther
}

// arrayItem identifies children of actual provider input and message arrays.
func (location wireLocation) arrayItem() wireLocation {
	switch location {
	case wireInputArray:
		return wireInputItem
	case wireMessagesArray:
		return wireMessage
	case wireOther, wireRoot, wireInputItem, wireMessage, wireDetails:
		return wireOther
	default:
		return wireOther
	}
}

// replayFragment retains exact provider-owned bytes and their permitted serialized slot.
type replayFragment struct {
	// location identifies the provider replay slot that may receive these bytes.
	location wireLocation
	// raw contains the complete exact original JSON fragment.
	raw []byte
}

// wireProjection owns private replay for one serialized raw attempt.
type wireProjection struct {
	// fragments maps non-secret local references to their private originals.
	fragments map[string]replayFragment
	// nonce prevents references from another invocation being accepted accidentally.
	nonce string
}

// projectWireBody replaces provider-owned replay with invocation-local references.
func projectWireBody(body []byte) (wireProjection, []byte, error) {
	projection := wireProjection{fragments: make(map[string]replayFragment), nonce: rand.Text()}
	safe, err := projection.rewrite(body, wireRoot, false, nil)
	return projection, safe, err
}

// restore validates every retained reference and restores its exact private fragment.
func (projection wireProjection) restore(body []byte) ([]byte, error) {
	if trimmed := bytes.TrimSpace(body); len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("serialized provider replacement must be a JSON object")
	}
	return projection.rewrite(body, wireRoot, true, make(map[string]bool))
}

// rewrite projects or validates a subtree while retaining exact private fragments.
func (projection wireProjection) rewrite(
	raw []byte,
	location wireLocation,
	restoring bool,
	seen map[string]bool,
) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("serialized provider body is empty")
	}
	switch raw[0] {
	case '{':
		return projection.rewriteObject(raw, location, restoring, seen)
	case '[':
		var array []jsontext.Value
		if err := json.Unmarshal(raw, &array); err != nil {
			return nil, fmt.Errorf("decode serialized provider array: %w", err)
		}
		for i, value := range array {
			replacement, err := projection.rewrite(value, location.arrayItem(), restoring, seen)
			if err != nil {
				return nil, err
			}
			array[i] = replacement
		}
		return encodeWireArray(array), nil
	default:
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("decode serialized provider value: %w", err)
		}
		return bytes.Clone(raw), nil
	}
}

// reference records exact private bytes and emits their non-secret local placeholder.
func (projection wireProjection) reference(raw []byte, location wireLocation) ([]byte, error) {
	id := projection.nonce + "-" + strconv.Itoa(len(projection.fragments))
	projection.fragments[id] = replayFragment{location: location, raw: bytes.Clone(raw)}
	return json.Marshal(map[string]string{replayReferenceKey: id})
}
