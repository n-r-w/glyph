package tui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// Controller decodes terminal keys before calling the application interaction owner.
type Controller struct {
	// interaction owns editor and selector transitions.
	interaction Interaction
}

// New connects terminal input to its application consumer.
func New(interaction Interaction) *Controller {
	return &Controller{interaction: interaction}
}

// Key decodes one framework key and returns any prepared asynchronous work.
func (controller *Controller) Key(key tea.Key) Work {
	return controller.interaction.Key(DecodeKey(key))
}

// Complete returns an I/O result to its serialized application owner.
func (controller *Controller) Complete(result Result) bool {
	return controller.interaction.Complete(result)
}

// DecodeKey discards framework metadata and preserves the interaction-relevant key and modifiers.
func DecodeKey(key tea.Key) Key {
	var modifiers Modifier
	if key.Mod&tea.ModCtrl != 0 {
		modifiers |= ModCtrl
	}
	if key.Mod&tea.ModShift != 0 {
		modifiers |= ModShift
	}
	if key.Mod&tea.ModAlt != 0 {
		modifiers |= ModAlt
	}
	if key.Mod&tea.ModMeta != 0 {
		modifiers |= ModMeta
	}
	if key.Mod & ^(tea.ModCtrl|tea.ModShift|tea.ModAlt|tea.ModMeta) != 0 {
		modifiers |= ModOther
	}
	return Key{Code: decodeCode(key.Code), Text: key.Text, Mod: modifiers}
}

// decodeCode maps framework special keys without mixing them with Unicode text.
func decodeCode(value rune) rune {
	code := value
	switch value {
	case tea.KeyEnter:
		code = KeyEnter
	case tea.KeyLeft:
		code = KeyLeft
	case tea.KeyRight:
		code = KeyRight
	case tea.KeyHome:
		code = KeyHome
	case tea.KeyEnd:
		code = KeyEnd
	case tea.KeyBackspace:
		code = KeyBackspace
	case tea.KeyDelete:
		code = KeyDelete
	case tea.KeyUp:
		code = KeyUp
	case tea.KeyDown:
		code = KeyDown
	case tea.KeyEscape:
		code = KeyEscape
	case tea.KeyTab:
		code = KeyTab
	default:
		if code < 0 || code > unicode.MaxRune {
			code = KeyOther
		}
	}
	return code
}
