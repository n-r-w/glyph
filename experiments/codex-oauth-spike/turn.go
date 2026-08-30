package main

import (
	"context"

	"fmt"

	"strings"

	"github.com/openai/openai-go/v3/responses"
)

// streamTurn consumes Responses SSE events and captures terminal replay data.
func streamTurn(
	ctx context.Context,
	client codexClient,
	request responses.ResponseNewParams,
) (streamedTurn, error) {
	stream := client.responses.NewStreaming(ctx, request)
	defer stream.Close()

	var result streamedTurn
	var text strings.Builder
	for stream.Next() {
		event := stream.Current()
		result.EventCount++
		switch event.Type {
		case "response.output_text.delta":
			delta := event.AsResponseOutputTextDelta().Delta
			text.WriteString(delta)
			result.TextDeltas++
		case "response.output_item.done":
			result.captureOutputItem(event.AsResponseOutputItemDone().Item)
		case "response.completed":
			completed := event.AsResponseCompleted().Response
			for _, item := range completed.Output {
				result.captureOutputItem(item)
			}
			result.IsCompleted = true
		case "response.failed":
			failed := event.AsResponseFailed().Response
			return streamedTurn{}, fmt.Errorf("Codex response failed with status %s", failed.Status)
		case "error":
			providerEvent := event.AsError()
			return streamedTurn{}, fmt.Errorf("Codex stream error %s: %s", providerEvent.Code, providerEvent.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return streamedTurn{}, normalizeProviderError(err, client.errors.ErrorBody())
	}
	result.Text = text.String()
	return result, nil
}

// captureOutputItem retains one reasoning item and one function call from completed events.
func (result *streamedTurn) captureOutputItem(item responses.ResponseOutputItemUnion) {
	result.OutputTypes = append(result.OutputTypes, item.Type)
	switch item.Type {
	case "reasoning":
		reasoning := item.AsReasoning()
		if result.Reasoning == nil || reasoning.EncryptedContent != "" {
			result.Reasoning = &reasoning
		}
	case "function_call":
		toolCall := item.AsFunctionCall()
		result.ToolCall = &toolCall
	}
}
