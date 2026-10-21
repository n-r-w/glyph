package compaction

import (
	"errors"
	"slices"
	"unicode/utf8"
)

// EstimateInput estimates complete outgoing content in model-neutral tokens.
func EstimateInput(history []Entry, instructions, tools string) int64 {
	total := estimateText(instructions) + estimateText(tools)
	for index := range history {
		total += history[index].EstimateTokens()
	}
	return total
}

// estimateText converts textual content size into a model-neutral estimate.
func estimateText(text string) int64 {
	const charactersPerToken = 4
	return (int64(utf8.RuneCountInString(text)) + charactersPerToken - 1) / charactersPerToken
}

// SelectRequest selects a complete recent suffix for compaction.
func SelectRequest(input Input, retained, contextTokens int64) (Request, error) {
	entries := make([]Entry, 0, len(input.History))
	for index := range input.History {
		entry := input.History[index]
		if entry.PreviousSummary {
			continue
		}
		entry.Tokens = entry.EstimateTokens()
		entries = append(entries, entry)
	}
	groups := findGroupStarts(entries)
	boundary, tokens := len(entries), int64(0)
	for _, start := range slices.Backward(groups) {
		if tokens >= retained && boundary < len(entries) {
			break
		}

		for position := start; position < boundary; position++ {
			tokens += entries[position].Tokens
		}
		boundary = start
	}
	if boundary == 0 || boundary == len(entries) {
		return Request{}, errors.New("compaction has no older reducible portion")
	}
	trigger, retry := input.Purpose.RequestIntent()
	return Request{
		Trigger: trigger, Retry: retry, Guidance: input.Guidance, Model: input.Model, Reasoning: input.Reasoning,
		Prefix: entries[:boundary], Suffix: entries[boundary:], Previous: input.Previous,
		ContextTokens: contextTokens, Estimated: true,
		Window: input.Model.Window, Reserve: input.Model.Reserve, Retained: retained,
	}, nil
}
