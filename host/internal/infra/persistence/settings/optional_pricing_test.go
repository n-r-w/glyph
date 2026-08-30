//go:build integration

package settings

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestLoadPreservesOptionalScalarYAML verifies boundary conversion for omitted, null, empty, and non-empty scalars.
func (s *SettingsSuite) TestLoadPreservesOptionalScalarYAML() {
	s.Run("omitted", func() {
		content := validSettings("")
		decoded := decodeSettingsFile(s.T(), content)
		s.True(decoded.ActiveUI.IsNone())
		s.True(decoded.Providers["compatible"].APIKey.IsNone())
		s.True(decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())

		loaded, err := New(writeSettings(s.T(), content)).Load()
		s.Require().NoError(err)
		s.True(loaded.ActiveUI.IsNone())
		s.True(loaded.Providers["compatible"].APIKey.IsNone())
		s.True(loaded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())
	})
	s.Run("null", func() {
		content := replace(validSettings(""), "providers:", "activeUI: null\nproviders:")
		content = replace(content, "    baseURL: https://example.com/v1", "    baseURL: https://example.com/v1\n    apiKey: null")
		content = replace(content, "          default: high\n      - id: plain", "          default: high\n          compatibilityKey: null\n      - id: plain")
		decoded := decodeSettingsFile(s.T(), content)
		s.True(decoded.ActiveUI.IsNone())
		s.True(decoded.Providers["compatible"].APIKey.IsNone())
		s.True(decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())

		loaded, err := New(writeSettings(s.T(), content)).Load()
		s.Require().NoError(err)
		s.True(loaded.ActiveUI.IsNone())
		s.True(loaded.Providers["compatible"].APIKey.IsNone())
		s.True(loaded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey.IsNone())
	})
	s.Run("empty", func() {
		testCases := map[string]struct {
			content string
			assert  func(settingsFile)
		}{
			"active UI": {
				content: replace(validSettings(""), "providers:", "activeUI: ''\nproviders:"),
				assert:  func(decoded settingsFile) { s.Equal(mo.Some(""), decoded.ActiveUI) },
			},
			"API key source": {
				content: replace(validSettings(""), "    baseURL: https://example.com/v1", "    baseURL: https://example.com/v1\n    apiKey:\n      literal: ''"),
				assert: func(decoded settingsFile) {
					apiKey, present := decoded.Providers["compatible"].APIKey.Get()
					s.Require().True(present)
					s.Equal(mo.Some(""), apiKey.Literal)
				},
			},
			"compatibility key": {
				content: replace(validSettings(""), "          default: high\n      - id: plain", "          default: high\n          compatibilityKey: ''\n      - id: plain"),
				assert: func(decoded settingsFile) {
					s.Equal(mo.Some(""), decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey)
				},
			},
		}
		for name, testCase := range testCases {
			s.Run(name, func() {
				testCase.assert(decodeSettingsFile(s.T(), testCase.content))
				_, err := New(writeSettings(s.T(), testCase.content)).Load()
				s.Require().Error(err)
			})
		}
	})
	s.Run("non-empty", func() {
		content := replace(validSettings(""), "providers:", "activeUI: glyph-tui\nproviders:")
		content = replace(content, "    baseURL: https://example.com/v1", "    baseURL: https://example.com/v1\n    apiKey:\n      literal: secret")
		content = replace(content, "          default: high\n      - id: plain", "          default: high\n          compatibilityKey: family\n      - id: plain")
		decoded := decodeSettingsFile(s.T(), content)
		s.Equal(mo.Some("glyph-tui"), decoded.ActiveUI)
		decodedAPIKey, present := decoded.Providers["compatible"].APIKey.Get()
		s.Require().True(present)
		s.Equal(mo.Some("secret"), decodedAPIKey.Literal)
		s.Equal(mo.Some("family"), decoded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey)

		loaded, err := New(writeSettings(s.T(), content)).Load()
		s.Require().NoError(err)
		s.Equal(mo.Some("glyph-tui"), loaded.ActiveUI)
		apiKey, present := loaded.Providers["compatible"].APIKey.Get()
		s.Require().True(present)
		s.Equal(mo.Some("secret"), apiKey.Literal)
		s.Equal(mo.Some("family"), loaded.Providers["compatible"].Models[0].Reasoning.CompatibilityKey)
	})
}

// TestLoadPreservesOptionalPricing verifies absent, flat, zero, and tiered model pricing.
func (s *SettingsSuite) TestLoadPreservesOptionalPricing() {
	// Arrange valid model pricing forms and their exact expected values.
	testCases := map[string]struct {
		yaml    string
		pricing mo.Option[model.Pricing]
	}{
		"absent": {yaml: "", pricing: mo.None[model.Pricing]()},
		"flat": {
			yaml: "        pricing:\n          input: 1.25\n          output: 10\n          cacheRead: 0.125\n          cacheWrite: 1.25",
			pricing: mo.Some(model.Pricing{
				Input: 1.25, Output: 10, CacheRead: 0.125, CacheWrite: 1.25, Tiers: nil,
			}),
		},
		"zero": {
			yaml: "        pricing:\n          input: 0\n          output: 0\n          cacheRead: 0\n          cacheWrite: 0",
			pricing: mo.Some(model.Pricing{
				Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0, Tiers: nil,
			}),
		},
		"tiered": {
			yaml: "        pricing:\n          input: 1\n          output: 2\n          cacheRead: 0.1\n          cacheWrite: 0.5\n          tiers:\n            - inputTokensAbove: 100\n              input: 3\n              output: 4\n              cacheRead: 0.3\n              cacheWrite: 0.7\n            - inputTokensAbove: 200\n              input: 5\n              output: 6\n              cacheRead: 0.5\n              cacheWrite: 0.9",
			pricing: mo.Some(model.Pricing{
				Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.5,
				Tiers: []model.PricingTier{
					{InputTokensAbove: 100, Input: 3, Output: 4, CacheRead: 0.3, CacheWrite: 0.7},
					{InputTokensAbove: 200, Input: 5, Output: 6, CacheRead: 0.5, CacheWrite: 0.9},
				},
			}),
		},
	}
	for name, testCase := range testCases {
		s.Run(name, func() {
			content := validSettings("")
			if testCase.yaml != "" {
				content = replace(content, "      - id: compatible", "      - id: compatible\n"+testCase.yaml)
			}

			// Act by loading the settings through the strict decoder and validator.
			settings, err := New(writeSettings(s.T(), content)).Load()

			// Assert pricing presence and configured values are preserved exactly.
			s.Require().NoError(err)
			s.Require().Equal(testCase.pricing, settings.Providers["compatible"].Models[0].Pricing)
		})
	}
}
