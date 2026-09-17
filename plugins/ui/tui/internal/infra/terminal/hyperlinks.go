package terminal

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const (
	// hyperlinkPrefix identifies terminal OSC 8 hyperlink metadata.
	hyperlinkPrefix = "\x1b]8;"
	// hyperlinkBEL terminates an OSC command using BEL.
	hyperlinkBEL = "\x07"
	// hyperlinkST terminates an OSC command using ST.
	hyperlinkST = "\x1b\\"
	// httpLinkScheme identifies unencrypted web links in displayed text.
	httpLinkScheme = "http://"
	// httpsLinkScheme identifies encrypted web links in displayed text.
	httpsLinkScheme = "https://"
)

// renderHyperlinks attaches complete targets before layout changes the visible text.
// Existing terminal links and other ANSI sequences are preserved rather than parsed as text.
func renderHyperlinks(text string) string {
	var result strings.Builder
	// linked marks text already covered by an explicit OSC 8 target.
	linked := false
	// start is the beginning of the current printable run.
	start := 0
	state := ansi.NormalState
	for offset := 0; offset < len(text); {
		sequence, width, size, next := ansi.DecodeSequence(text[offset:], state, nil)
		state = next
		if width == 0 {
			result.WriteString(renderLinkText(text[start:offset], linked))
			result.WriteString(sequence)
			if destination, ok := hyperlinkDestination(sequence); ok {
				linked = destination != ""
			}
			start = offset + size
		}
		offset += size
	}
	result.WriteString(renderLinkText(text[start:], linked))
	return result.String()
}

// renderLinkText recognizes web addresses and inline Markdown links in a printable run.
func renderLinkText(text string, linked bool) string {
	if linked {
		return text
	}
	var result strings.Builder
	for offset := 0; offset < len(text); {
		if text[offset] == '[' {
			label, target, size := markdownLink(text[offset:])
			if size > 0 {
				result.WriteString(ansi.SetHyperlink(target))
				result.WriteString(label)
				result.WriteString(ansi.ResetHyperlink())
				offset += size
				continue
			}
		}
		if text[offset] == 'h' || text[offset] == 'H' {
			if target := webLink(text[offset:], true); target != "" {
				result.WriteString(ansi.SetHyperlink(target))
				result.WriteString(target)
				result.WriteString(ansi.ResetHyperlink())
				offset += len(target)
				continue
			}
		}
		result.WriteByte(text[offset])
		offset++
	}
	return result.String()
}

// markdownLink reads a labeled inline link without changing other Markdown syntax.
func markdownLink(text string) (label, destination string, size int) {
	labelEnd := strings.Index(text, "](")
	if labelEnd <= 1 || strings.ContainsAny(text[1:labelEnd], "[]") {
		return "", "", 0
	}
	targetStart := labelEnd + len("](")
	target := webLink(text[targetStart:], false)
	targetEnd := targetStart + len(target)
	if target == "" || targetEnd >= len(text) || text[targetEnd] != ')' {
		return "", "", 0
	}
	return text[1:labelEnd], target, targetEnd + 1
}

// webLink extracts an HTTP(S) destination while excluding prose delimiters.
// Balanced parentheses and brackets remain part of paths and IPv6 addresses.
func webLink(text string, prose bool) string {
	if !hasWebScheme(text) {
		return ""
	}
	end := len(text)
	parentheses, brackets := 0, 0
	for index, character := range text {
		if unicode.IsSpace(character) || unicode.IsControl(character) || strings.ContainsRune("<>\"'`\\", character) {
			end = index
			break
		}
		switch character {
		case '(':
			parentheses++
		case ')':
			parentheses--
		case '[':
			brackets++
		case ']':
			brackets--
		}
		if parentheses < 0 || brackets < 0 {
			end = index
			break
		}
	}
	target := text[:end]
	if prose {
		target = strings.TrimRight(target, ".,;:!?")
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	return target
}

// hasWebScheme recognizes web schemes without changing the target's case or escaping.
func hasWebScheme(text string) bool {
	return len(text) >= len(httpLinkScheme) && strings.EqualFold(text[:len(httpLinkScheme)], httpLinkScheme) ||
		len(text) >= len(httpsLinkScheme) && strings.EqualFold(text[:len(httpsLinkScheme)], httpsLinkScheme)
}

// hyperlinkDestination extracts a target from OSC 8 without interpreting other controls.
func hyperlinkDestination(sequence string) (string, bool) {
	if !strings.HasPrefix(sequence, hyperlinkPrefix) {
		return "", false
	}
	body := strings.TrimSuffix(strings.TrimSuffix(sequence, hyperlinkBEL), hyperlinkST)
	_, destination, found := strings.Cut(strings.TrimPrefix(body, hyperlinkPrefix), ";")
	return destination, found
}

// independentHyperlinkLines closes and reopens links at visual row boundaries.
// A clipped viewport can then display any row without needing preceding OSC metadata.
func independentHyperlinkLines(text string) []string {
	lines := strings.Split(text, "\n")
	// opening retains the complete OSC sequence, including any existing link parameters.
	opening := ""
	for index, line := range lines {
		var result strings.Builder
		result.WriteString(opening)
		state := ansi.NormalState
		for line != "" {
			sequence, _, size, next := ansi.DecodeSequence(line, state, nil)
			state = next
			line = line[size:]
			result.WriteString(sequence)
			if destination, ok := hyperlinkDestination(sequence); ok {
				opening = sequence
				if destination == "" {
					opening = ""
				}
			}
		}
		if opening != "" {
			result.WriteString(ansi.ResetHyperlink())
		}
		lines[index] = result.String()
	}
	return lines
}
