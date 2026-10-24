package sessions

// isLineBreak identifies the delimiters used in session names and preview selection.
func isLineBreak(value rune) bool {
	return value == '\r' || value == '\n'
}
