// Package compaction defines extension-owned context and composition values.
package compaction

import (
	"errors"

	"github.com/samber/mo"
)

// Participant identifies one captured extension capability.
type Participant struct {
	// Extension identifies its runtime owner.
	Extension string
	// Runtime identifies the captured extension runtime.
	Runtime string
	// Handler identifies its extension-local capability.
	Handler string
	// Order identifies its registration position.
	Order int64
}

// Input contains immutable conversation data for policy decisions.
type Input struct {
	// Binding identifies the issued session access.
	Binding Binding
	// Entries contains the original active branch.
	Entries []Entry
	// History contains prepared model history.
	History []Entry
	// Previous contains the preceding replacement.
	Previous mo.Option[Result]
	// Model describes the conversation model.
	Model Model
	// Reasoning contains the conversation reasoning choice.
	Reasoning string
	// Instructions contains captured conversation instructions.
	Instructions string
	// ToolText contains the textual and JSON tool-definition contribution.
	ToolText string
	// ToolShape identifies the captured tool definitions for continuation matching.
	ToolShape string
	// Guidance contains manual caller guidance.
	Guidance string
	// Purpose identifies the reason for policy preparation.
	Purpose Purpose
	// Requests contains request-handler registrations.
	Requests []Participant
	// Generators contains captured generator registrations.
	Generators []Participant
	// Results contains result-handler registrations.
	Results []Participant
}

// NeedsCompaction identifies captured input that requires replacement.
func (i Input) NeedsCompaction(tokens int64) bool {
	switch i.Purpose {
	case PurposeManual:
		return true
	case PurposeAutomatic:
		return tokens > i.Model.Window-i.Model.Reserve
	case PurposeOverflow, PurposeUnspecified, PurposeInvalid:
		return false
	}
	return false
}

// State contains the original request and last accepted composition values.
type State struct {
	// Original contains the immutable initial request.
	Original Request
	// Current contains the accepted request state.
	Current Request
	// Result contains the accepted optional result.
	Result mo.Option[Result]
}

// RequestAction contains an independent request and optional-result update.
type RequestAction struct {
	// Cancel is an exclusive cancellation decision.
	Cancel bool
	// RequestDisposition is the request update decision.
	RequestDisposition RequestDisposition
	// Request contains the replacement request.
	Request mo.Option[Request]
	// ResultDisposition is the result update decision.
	ResultDisposition ResultDisposition
	// Result contains the replacement result.
	Result mo.Option[Result]
}

// Validate checks independent request and result updates.
func (a RequestAction) Validate() error {
	if a.Cancel {
		if a.RequestDisposition != RequestUnspecified || a.ResultDisposition != ResultUnspecified ||
			a.Request.IsSome() ||
			a.Result.IsSome() {
			return errors.New("compaction cancellation must be exclusive")
		}
		return nil
	}
	if a.RequestDisposition != RequestPreserve && a.RequestDisposition != RequestReplace {
		return errors.New("invalid compaction request disposition")
	}
	if (a.RequestDisposition == RequestReplace) != a.Request.IsSome() {
		return errors.New("compaction request replacement payload does not match disposition")
	}
	if request, present := a.Request.Get(); present {
		if err := request.Validate(); err != nil {
			return err
		}
	}
	return validateResultDisposition(a.ResultDisposition, a.Result.IsSome(), true)
}

// ResultAction contains an update to a complete result.
type ResultAction struct {
	// Cancel is an exclusive cancellation decision.
	Cancel bool
	// Disposition is the result update decision.
	Disposition ResultDisposition
	// Result contains the replacement result.
	Result mo.Option[Result]
}

// Validate checks an update to an existing result.
func (a ResultAction) Validate() error {
	if a.Cancel {
		if a.Disposition != ResultUnspecified || a.Result.IsSome() {
			return errors.New("compaction cancellation must be exclusive")
		}
		return nil
	}
	return validateResultDisposition(a.Disposition, a.Result.IsSome(), false)
}

// validateResultDisposition checks payload presence for a result update.
func validateResultDisposition(disposition ResultDisposition, present, optional bool) error {
	if disposition != ResultPreserve && disposition != ResultReplace && (!optional || disposition != ResultClear) {
		return errors.New("invalid compaction result disposition")
	}
	if (disposition == ResultReplace) != present {
		return errors.New("compaction result replacement payload does not match disposition")
	}
	return nil
}
