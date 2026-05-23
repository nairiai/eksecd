package claude

import (
	"nairid/services"
)

// extractClaudeUsage builds a CLIAgentUsage from a stream of Claude messages.
// It pulls per-LLM-call token counts from "assistant" events (summed across
// the whole session) and pulls total_cost_usd from the final "result" event
// when present. Cache reads and cache creation are tracked separately to
// preserve granularity in the API.
//
// Returns nil if no usage data is found, so the caller can leave cost fields
// nil upstream rather than persisting zeros that would misleadingly imply
// a free call.
func extractClaudeUsage(messages []services.ClaudeMessage) *services.CLIAgentUsage {
	var (
		inputTokens  int64
		outputTokens int64
		cacheRead    int64
		cacheCreate  int64
		hasUsage     bool

		costUSD    float64
		hasCostUSD bool

		model string
	)

	for _, msg := range messages {
		switch m := msg.(type) {
		case services.AssistantMessage:
			u := m.Message.Usage
			if u.InputTokens > 0 || u.OutputTokens > 0 ||
				u.CacheReadInputTokens > 0 || u.CacheCreationInputTokens > 0 {
				hasUsage = true
				inputTokens += u.InputTokens
				outputTokens += u.OutputTokens
				cacheRead += u.CacheReadInputTokens
				cacheCreate += u.CacheCreationInputTokens
			}
			if model == "" && m.Message.Model != "" {
				model = m.Message.Model
			}
		case services.ResultMessage:
			if m.TotalCostUsd > 0 {
				costUSD = m.TotalCostUsd
				hasCostUSD = true
			}
			// ResultMessage usage is the per-session rollup. Prefer it over
			// the per-call sum when present, since it accounts for cases the
			// stream parser might have missed.
			u := m.Usage
			if u.InputTokens > 0 || u.OutputTokens > 0 ||
				u.CacheReadInputTokens > 0 || u.CacheCreationInputTokens > 0 {
				hasUsage = true
				inputTokens = u.InputTokens
				outputTokens = u.OutputTokens
				cacheRead = u.CacheReadInputTokens
				cacheCreate = u.CacheCreationInputTokens
			}
		}
	}

	if !hasUsage && !hasCostUSD {
		return nil
	}

	usage := &services.CLIAgentUsage{Model: model}
	if hasCostUSD {
		c := costUSD
		usage.CostUSD = &c
	}
	if hasUsage {
		in := inputTokens
		out := outputTokens
		cr := cacheRead
		cw := cacheCreate
		usage.InputTokens = &in
		usage.OutputTokens = &out
		usage.CacheReadTokens = &cr
		usage.CacheWriteTokens = &cw
	}
	return usage
}
