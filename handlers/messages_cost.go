package handlers

import (
	"nairid/models"
	"nairid/services"
)

// applyUsageToAssistantPayload copies optional cost / token fields from a
// CLIAgentResult.Usage onto an outgoing AssistantMessagePayload so the
// backend can persist them on the conversation_messages row. Nil-safe:
// when usage is nil (e.g. Cursor, or extraction failure) the payload is
// left as-is and the backend stores NULL for every cost column.
func applyUsageToAssistantPayload(p *models.AssistantMessagePayload, usage *services.CLIAgentUsage) {
	if p == nil || usage == nil {
		return
	}
	p.CostUSD = usage.CostUSD
	p.InputTokens = usage.InputTokens
	p.OutputTokens = usage.OutputTokens
	p.CacheReadTokens = usage.CacheReadTokens
	p.CacheWriteTokens = usage.CacheWriteTokens
	if usage.Model != "" {
		m := usage.Model
		p.Model = &m
	}
}
