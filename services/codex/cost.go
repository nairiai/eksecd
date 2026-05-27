package codex

import (
	"nairid/services"
	"nairid/services/pricing"
)

// extractCodexUsage builds a CLIAgentUsage from a stream of Codex messages.
//
// Codex emits a turn.completed event after every LLM call inside a session
// with a `usage` block (input_tokens, cached_input_tokens, output_tokens).
// We sum across all turns to get session-level totals.
//
// Cost is computed via the local pricing table (nairid/services/pricing)
// since the Codex CLI itself does not report a dollar cost. If the model is
// unknown to the pricing table the usage is still returned but CostUSD stays nil.
//
// Returns nil if no turn.completed events were seen, so callers can leave
// cost columns NULL upstream rather than persisting zeros.
func extractCodexUsage(messages []CodexMessage, model string) *services.CLIAgentUsage {
	var (
		inputTokens       int64
		cachedInputTokens int64
		outputTokens      int64
		seenAny           bool
	)

	for _, msg := range messages {
		t, ok := msg.(TurnCompletedMessage)
		if !ok {
			continue
		}
		if t.Usage.InputTokens == 0 && t.Usage.CachedInputTokens == 0 && t.Usage.OutputTokens == 0 {
			continue
		}
		seenAny = true
		inputTokens += int64(t.Usage.InputTokens)
		cachedInputTokens += int64(t.Usage.CachedInputTokens)
		outputTokens += int64(t.Usage.OutputTokens)
	}

	if !seenAny {
		return nil
	}

	in := inputTokens
	out := outputTokens
	cr := cachedInputTokens
	// Codex's turn.completed events only report cached_input_tokens (= cache
	// reads); the CLI does not surface cache-write counts. Leave CacheWriteTokens
	// nil to signal "unknown" rather than hardcoding 0, which would falsely
	// imply we observed zero cache writes.
	usage := &services.CLIAgentUsage{
		InputTokens:     &in,
		OutputTokens:    &out,
		CacheReadTokens: &cr,
	}
	if model != "" {
		m := model
		usage.Model = &m
	}

	if cost, ok := pricing.CodexCostUSD(model, inputTokens, cachedInputTokens, outputTokens); ok {
		c := cost
		usage.CostUSD = &c
	}

	return usage
}
