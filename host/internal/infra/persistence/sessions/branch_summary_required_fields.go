package sessions

import "bytes"

const (
	// fieldSummarySource identifies the persisted summary producer object.
	fieldSummarySource = "source"
	// fieldSummaryExtensionID identifies the non-model producer alternative.
	fieldSummaryExtensionID = "extension_id"
	// fieldSummaryProvider identifies the producing model's provider.
	fieldSummaryProvider = "provider"
	// fieldSummaryReasoning identifies the producing model's reasoning choice.
	fieldSummaryReasoning = "reasoningChoice"
	// fieldSummaryUsage identifies the nested reported model usage.
	fieldSummaryUsage = "usage"
	// fieldSummaryCacheRead identifies cached input in normalized usage.
	fieldSummaryCacheRead = "cacheReadTokens"
	// summaryJSONNull marks an absent source alternative in persisted JSON.
	summaryJSONNull = "null"
)

// validateSummarySourceRequiredFields requires source alternatives and complete reported model metadata.
func (entry jsonObject) validateSummarySourceRequiredFields() error {
	// Both alternative keys are required, but one must contain JSON null.
	source, err := entry.requiredChild(fieldSummarySource, fieldSummaryExtensionID, fieldModel)
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(source[fieldModel]), []byte(summaryJSONNull)) {
		return nil
	}
	// A present model source must retain every identity field across restart.
	fields := []string{fieldSummaryProvider, fieldModel, fieldSummaryReasoning}
	modelSource, err := source.requiredChild(fieldModel, fields...)
	if err != nil {
		return err
	}
	if validationErr := modelSource.requireNonNullFields(fields...); validationErr != nil {
		return validationErr
	}
	// Reported usage requires all buckets, including explicit zero counters.
	usageFields := []string{
		fieldInputTokens,
		fieldOutputTokens,
		fieldSummaryCacheRead,
		fieldCacheWriteTokens,
		fieldReasoningTokens,
		fieldTotalTokens,
	}
	return modelSource.validateOptionalRequiredObject(fieldSummaryUsage, usageFields, usageFields)
}
