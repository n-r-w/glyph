package sessions

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestModelRecordRoundTripsEstimatedCostPresence verifies absent, zero, and nonzero persisted cost.
func TestModelRecordRoundTripsEstimatedCostPresence(t *testing.T) {
	t.Parallel()

	// Arrange terminal model entries with each supported cost presence state.
	testCases := map[string]mo.Option[session.EstimatedCost]{
		"absent": mo.None[session.EstimatedCost](),
		"zero": mo.Some(session.EstimatedCost{
			Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Total: 0,
		}),
		"nonzero": mo.Some(session.EstimatedCost{
			Input: 0.1, Output: 0.2, CacheRead: 0.03, CacheWrite: 0.04, Total: 0.37,
		}),
	}
	for name, estimatedCost := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			entry := session.Entry{ParentID: mo.None[string](), ID: "model-entry", CreatedAt: time.Unix(10, 0).UTC(),
				Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
				Model: mo.Some(model.Response{
					Content: nil, Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
					Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
					ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
					Usage: mo.Some(model.Usage{}), Diagnostics: nil,
				}),
				EstimatedCost: estimatedCost, ToolResult: mo.None[session.ToolResult](),
				Extension: mo.None[session.ExtensionEnvelope](), BranchSummary: mo.None[session.BranchSummaryEntry](),
			}

			// Act by encoding and decoding one strict model record.
			encoded, encodeErr := encodeEntry(entry)
			decoded, decodeErr := decodeEntry(encoded)

			// Assert exact cost presence and values survive and absence is omitted from JSON.
			require.NoError(t, encodeErr)
			require.NoError(t, decodeErr)
			assert.Equal(t, entry, decoded)
			var record map[string]any
			require.NoError(t, json.Unmarshal(encoded, &record))
			_, present := record["estimatedCost"]
			assert.Equal(t, estimatedCost.IsSome(), present)
		})
	}
}
