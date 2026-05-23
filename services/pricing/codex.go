package pricing

import "strings"

// codexModelPricing holds per-million-token rates in USD for an OpenAI/Codex model.
// Cached input tokens are billed at a discount versus fresh input tokens.
type codexModelPricing struct {
	InputUSDPerMillion       float64
	CachedInputUSDPerMillion float64
	OutputUSDPerMillion      float64
}

// Public OpenAI pricing as of 2026-05. Values may drift; treat this as a
// best-effort estimate. Pricing updates are a nairid release.
//
// Sources:
//   - https://openai.com/api/pricing (gpt-5, gpt-5-codex families)
//   - https://platform.openai.com/docs/models
var codexPricingByModel = map[string]codexModelPricing{
	// GPT-5 family
	"gpt-5":      {InputUSDPerMillion: 1.25, CachedInputUSDPerMillion: 0.125, OutputUSDPerMillion: 10.00},
	"gpt-5-mini": {InputUSDPerMillion: 0.25, CachedInputUSDPerMillion: 0.025, OutputUSDPerMillion: 2.00},
	"gpt-5-nano": {InputUSDPerMillion: 0.05, CachedInputUSDPerMillion: 0.005, OutputUSDPerMillion: 0.40},

	// Codex variants of GPT-5 (priced like their base model)
	"gpt-5-codex":      {InputUSDPerMillion: 1.25, CachedInputUSDPerMillion: 0.125, OutputUSDPerMillion: 10.00},
	"gpt-5-mini-codex": {InputUSDPerMillion: 0.25, CachedInputUSDPerMillion: 0.025, OutputUSDPerMillion: 2.00},

	// Legacy o-series fallbacks (in case older sessions still run)
	"o3":      {InputUSDPerMillion: 2.00, CachedInputUSDPerMillion: 0.50, OutputUSDPerMillion: 8.00},
	"o3-mini": {InputUSDPerMillion: 1.10, CachedInputUSDPerMillion: 0.55, OutputUSDPerMillion: 4.40},
	"o4-mini": {InputUSDPerMillion: 1.10, CachedInputUSDPerMillion: 0.275, OutputUSDPerMillion: 4.40},
}

// CodexCostUSD computes the dollar cost of a single Codex turn from its
// reported token usage. cachedInputTokens are subtracted from inputTokens
// to avoid double-billing, then billed at the discounted cache rate.
//
// Returns (cost, true) when the model is recognised. Returns (0, false)
// when the model is unknown so callers can leave cost NULL upstream.
func CodexCostUSD(model string, inputTokens, cachedInputTokens, outputTokens int64) (float64, bool) {
	p, ok := lookupCodexPricing(model)
	if !ok {
		return 0, false
	}

	// Defensive clamp: cached can never exceed reported input.
	freshInput := inputTokens - cachedInputTokens
	if freshInput < 0 {
		freshInput = 0
	}
	if cachedInputTokens < 0 {
		cachedInputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}

	const perMillion = 1_000_000.0
	cost := (float64(freshInput)/perMillion)*p.InputUSDPerMillion +
		(float64(cachedInputTokens)/perMillion)*p.CachedInputUSDPerMillion +
		(float64(outputTokens)/perMillion)*p.OutputUSDPerMillion
	return cost, true
}

// lookupCodexPricing returns the pricing entry for a model, accepting both
// canonical model ids ("gpt-5-codex") and snapshot/date-stamped variants
// ("gpt-5-codex-2026-04-01") by prefix matching.
func lookupCodexPricing(model string) (codexModelPricing, bool) {
	if p, ok := codexPricingByModel[model]; ok {
		return p, true
	}
	// Prefix match for snapshot-suffixed model ids. Try longest first so
	// "gpt-5-codex-2026-04-01" matches "gpt-5-codex" before "gpt-5".
	var best string
	for prefix := range codexPricingByModel {
		if !strings.HasPrefix(model, prefix+"-") {
			continue
		}
		if len(prefix) > len(best) {
			best = prefix
		}
	}
	if best == "" {
		return codexModelPricing{}, false
	}
	return codexPricingByModel[best], true
}
