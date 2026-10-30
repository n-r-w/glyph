package openai

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
)

// rewriteObject maps public child values and recognizes only actual provider-owned replay slots.
func (projection wireProjection) rewriteObject(
	raw []byte,
	location wireLocation,
	restoring bool,
	seen map[string]bool,
) ([]byte, error) {
	var object map[string]jsontext.Value
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("decode serialized provider object: %w", err)
	}
	if _, present := object[replayReferenceKey]; present {
		if !restoring {
			return nil, errors.New("provider body contains a reserved replay reference")
		}
		return projection.restoreReference(object, location, seen)
	}
	if !restoring && location == wireInputItem {
		var kind string
		if json.Unmarshal(object[wireTypeField], &kind) == nil && kind == wireReasoningType {
			return projection.reference(raw, location)
		}
	}
	for key, value := range object {
		childLocation := location.objectChild(key, object)
		var replacement []byte
		var err error
		if !restoring && childLocation == wireDetails {
			replacement, err = projection.reference(value, childLocation)
		} else {
			replacement, err = projection.rewrite(value, childLocation, restoring, seen)
		}
		if err != nil {
			return nil, err
		}
		object[key] = replacement
	}
	return encodeWireObject(object)
}

// restoreReference accepts a single unchanged invocation-local reference in its provider replay slot.
func (projection wireProjection) restoreReference(
	object map[string]jsontext.Value,
	location wireLocation,
	seen map[string]bool,
) ([]byte, error) {
	var id string
	if len(object) != 1 {
		return nil, errors.New("malformed private replay reference")
	}
	if err := json.Unmarshal(object[replayReferenceKey], &id); err != nil {
		return nil, fmt.Errorf("decode private replay reference: %w", err)
	}
	fragment, known := projection.fragments[id]
	if !known {
		return nil, errors.New("unknown private replay reference")
	}
	if fragment.location != location {
		return nil, errors.New("misplaced private replay reference")
	}
	if seen[id] {
		return nil, errors.New("duplicated private replay reference")
	}
	seen[id] = true
	return bytes.Clone(fragment.raw), nil
}
