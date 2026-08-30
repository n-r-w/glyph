package main

import (
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// compileReadSchema verifies the selected validator and returns both provider and runtime forms.
func compileReadSchema() (*jsonschema.Schema, map[string]any, error) {
	var schemaMap map[string]any
	if err := json.Unmarshal([]byte(readSchemaJSON), &schemaMap); err != nil {
		return nil, nil, fmt.Errorf("decode read schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(readSchemaURL, schemaMap); err != nil {
		return nil, nil, fmt.Errorf("add read schema resource: %w", err)
	}
	compiled, err := compiler.Compile(readSchemaURL)
	if err != nil {
		return nil, nil, fmt.Errorf("compile read schema: %w", err)
	}
	return compiled, schemaMap, nil
}

// newCodexClient configures openai-go only as the Codex SSE transport.
func newCodexClient(accessToken, accountID string) codexClient {
	errorTransport := &errorCaptureTransport{base: http.DefaultTransport}
	httpClient := &http.Client{Transport: errorTransport}
	return codexClient{
		responses: responses.NewResponseService(
			option.WithBaseURL(codexBaseURL),
			option.WithAPIKey(accessToken),
			option.WithHeader("chatgpt-account-id", accountID),
			option.WithHeader("OpenAI-Beta", "responses=experimental"),
			option.WithHeader("originator", "glyph"),
			option.WithHeader("User-Agent", "glyph-codex-spike/1"),
			option.WithMaxRetries(0),
			option.WithHTTPClient(httpClient),
		),
		errors: errorTransport,
	}
}

// readTools constructs the exact strict function tool advertised in both turns.
func readTools(schema map[string]any) []responses.ToolUnionParam {
	return []responses.ToolUnionParam{{
		OfFunction: &responses.FunctionToolParam{
			Name:        "read",
			Description: param.NewOpt("Read the complete contents of the generated sample text file."),
			Parameters:  schema,
			Strict:      param.NewOpt(true),
		},
	}}
}

// baseRequest applies the stateless Codex policy shared by both model turns.
func baseRequest(tools []responses.ToolUnionParam) responses.ResponseNewParams {
	return responses.ResponseNewParams{
		Model: shared.ResponsesModel(modelID),
		Instructions: param.NewOpt(
			"You are validating a safe Glyph tool-calling integration. Follow the requested tool flow exactly.",
		),
		Store:             param.NewOpt(false),
		ParallelToolCalls: param.NewOpt(false),
		Include: []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		},
		Reasoning: shared.ReasoningParam{
			Effort:  shared.ReasoningEffortHigh,
			Summary: shared.ReasoningSummaryAuto,
		},
		Tools: tools,
	}
}

// firstRequest forces one read call so the experiment deterministically exercises tool streaming.
func firstRequest(tools []responses.ToolUnionParam) responses.ResponseNewParams {
	request := baseRequest(tools)
	request.Input = responses.ResponseNewParamsInputUnion{
		OfInputItemList: responses.ResponseInputParam{{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfString: param.NewOpt(firstPrompt),
				},
			},
		}},
	}
	request.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
		OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsRequired),
	}
	return request
}

// secondRequest replays the encrypted reasoning item and returns the tool result statelessly.
func secondRequest(
	tools []responses.ToolUnionParam,
	reasoning responses.ResponseReasoningItem,
	toolCall responses.ResponseFunctionToolCall,
	toolOutput string,
) (responses.ResponseNewParams, error) {
	if reasoning.ID == "" || reasoning.EncryptedContent == "" {
		return responses.ResponseNewParams{}, fmt.Errorf("reasoning replay data is incomplete")
	}

	summaries := make([]responses.ResponseReasoningItemSummaryParam, len(reasoning.Summary))
	for index, summary := range reasoning.Summary {
		summaries[index] = responses.ResponseReasoningItemSummaryParam{Text: summary.Text}
	}

	input := responses.ResponseInputParam{
		{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfString: param.NewOpt(firstPrompt),
				},
			},
		},
		{
			OfReasoning: &responses.ResponseReasoningItemParam{
				ID:               reasoning.ID,
				Summary:          summaries,
				EncryptedContent: param.NewOpt(reasoning.EncryptedContent),
			},
		},
		{
			OfFunctionCall: &responses.ResponseFunctionToolCallParam{
				Arguments: toolCall.Arguments,
				CallID:    toolCall.CallID,
				Name:      toolCall.Name,
			},
		},
		{
			OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
				CallID: toolCall.CallID,
				Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
					OfString: param.NewOpt(toolOutput),
				},
			},
		},
	}

	request := baseRequest(tools)
	request.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: input}
	request.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
		OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsNone),
	}
	request.Instructions = param.NewOpt(
		"Report the exact tool output marker in the final answer. Do not call another tool.",
	)
	return request, nil
}
