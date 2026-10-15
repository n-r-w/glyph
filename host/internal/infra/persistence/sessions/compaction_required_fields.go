package sessions

import "encoding/json/jsontext"

// validateCompactionRequestRequiredFields checks a saved request's identity and optional accounting.
func validateCompactionRequestRequiredFields(raw jsontext.Value) error {
	const fieldEstimatedCost = "estimatedCost"

	fields := []string{fieldSummaryProvider, fieldModel, fieldSummaryReasoning}
	request, err := requiredJSONObject(raw, fields...)
	if err != nil {
		return err
	}
	if validationErr := request.requireNonNullFields(fields...); validationErr != nil {
		return validationErr
	}
	usageFields := []string{
		fieldInputTokens,
		fieldOutputTokens,
		fieldSummaryCacheRead,
		fieldCacheWriteTokens,
		fieldReasoningTokens,
		fieldTotalTokens,
	}
	if validationErr := request.validateOptionalRequiredObject(
		fieldSummaryUsage,
		usageFields,
		usageFields,
	); validationErr != nil {
		return validationErr
	}
	costFields := []string{fieldInput, fieldOutput, fieldCacheRead, fieldCacheWrite, fieldTotal}
	return request.validateOptionalRequiredObject(fieldEstimatedCost, costFields, costFields)
}
