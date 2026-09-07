// Package tui decodes terminal input into framework-neutral interactions.
package tui

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=tui

// Interaction receives serialized editor actions and command acknowledgements.
type Interaction interface {
	Key(Key) Work
	Complete(Result) bool
}

// Work executes one prepared command without changing application state.
type Work interface {
	Execute() Result
}

// Result returns command I/O to the event loop that owns application state.
type Result struct {
	// ID identifies the prepared command.
	ID string
	// Err preserves the complete dispatch failure.
	Err error
}

// Key contains a decoded key and its text without framework event metadata.
type Key struct {
	// Code identifies a special key or the typed Unicode code point.
	Code rune
	// Text contains the text inserted by an unmodified key.
	Text string
	// Mod identifies held modifier keys.
	Mod Modifier
}

// Modifier identifies the terminal modifiers used by interaction rules.
type Modifier uint8

const (
	// ModCtrl identifies the control modifier.
	ModCtrl Modifier = 1 << iota
	// ModShift identifies the shift modifier.
	ModShift
	// ModAlt identifies the alternate modifier.
	ModAlt
	// ModMeta identifies the meta modifier.
	ModMeta
	// ModOther preserves extra modifiers so exact shortcut matching does not discard them.
	ModOther
)

const (
	// KeyEnter confirms input or selection.
	KeyEnter rune = -1 - iota
	// KeyLeft moves the editor cursor or folds a tree branch.
	KeyLeft
	// KeyRight moves the editor cursor or unfolds a tree branch.
	KeyRight
	// KeyHome moves to the first editor position.
	KeyHome
	// KeyEnd moves to the last editor position.
	KeyEnd
	// KeyBackspace removes the preceding rune.
	KeyBackspace
	// KeyDelete removes the following rune.
	KeyDelete
	// KeyUp selects the preceding row.
	KeyUp
	// KeyDown selects the following row.
	KeyDown
	// KeyEscape cancels the focused interaction.
	KeyEscape
	// KeyTab identifies tab-based selection shortcuts.
	KeyTab
	// KeyOther identifies an unmapped framework special key.
	KeyOther
)
