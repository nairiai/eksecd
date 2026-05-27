package claude

import (
	"encoding/json"
	"testing"

	"nairid/services"
)

func newAssistant(input, output, cacheRead, cacheWrite int64, model string) services.AssistantMessage {
	var a services.AssistantMessage
	a.Type = "assistant"
	a.SessionID = "session_test"
	a.Message.ID = "msg_assist"
	a.Message.Type = "message"
	a.Message.Model = model
	a.Message.Content = []json.RawMessage{}
	a.Message.StopReason = "end_turn"
	a.Message.Usage = services.ClaudeUsage{
		InputTokens:              input,
		OutputTokens:             output,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheWrite,
	}
	return a
}

func newResult(costUSD float64) services.ResultMessage {
	return services.ResultMessage{
		Type:         "result",
		Subtype:      "success",
		IsError:      false,
		Result:       "all done",
		SessionID:    "session_test",
		TotalCostUsd: costUSD,
	}
}

func TestExtractClaudeUsage_NoMessages(t *testing.T) {
	if got := extractClaudeUsage(nil); got != nil {
		t.Fatalf("expected nil usage from empty messages, got %+v", got)
	}
}

func TestExtractClaudeUsage_AssistantOnly_SumsAcrossCalls(t *testing.T) {
	msgs := []services.ClaudeMessage{
		newAssistant(100, 50, 200, 0, "claude-sonnet-4-5-20250929"),
		newAssistant(10, 5, 50, 100, "claude-sonnet-4-5-20250929"),
	}
	got := extractClaudeUsage(msgs)
	if got == nil {
		t.Fatal("expected non-nil usage")
	}
	if got.CostUSD != nil {
		t.Fatalf("expected nil cost without ResultMessage, got %v", *got.CostUSD)
	}
	if got.InputTokens == nil || *got.InputTokens != 110 {
		t.Fatalf("InputTokens want 110, got %v", got.InputTokens)
	}
	if got.OutputTokens == nil || *got.OutputTokens != 55 {
		t.Fatalf("OutputTokens want 55, got %v", got.OutputTokens)
	}
	if got.CacheReadTokens == nil || *got.CacheReadTokens != 250 {
		t.Fatalf("CacheReadTokens want 250, got %v", got.CacheReadTokens)
	}
	if got.CacheWriteTokens == nil || *got.CacheWriteTokens != 100 {
		t.Fatalf("CacheWriteTokens want 100, got %v", got.CacheWriteTokens)
	}
	if got.Model == nil || *got.Model != "claude-sonnet-4-5-20250929" {
		t.Fatalf("Model = %v, want claude-sonnet-4-5-20250929", got.Model)
	}
}

func TestExtractClaudeUsage_ResultUsagePreferred(t *testing.T) {
	// When a ResultMessage carries its own usage rollup it wins over the
	// per-assistant-event sum.
	msgs := []services.ClaudeMessage{
		newAssistant(100, 50, 200, 0, "claude-sonnet-4-5-20250929"),
		func() services.ResultMessage {
			r := newResult(0.045)
			r.Usage = services.ClaudeUsage{
				InputTokens:              999,
				OutputTokens:             888,
				CacheReadInputTokens:     777,
				CacheCreationInputTokens: 666,
			}
			return r
		}(),
	}
	got := extractClaudeUsage(msgs)
	if got == nil {
		t.Fatal("expected non-nil usage")
	}
	if got.CostUSD == nil || *got.CostUSD != 0.045 {
		t.Fatalf("CostUSD = %v, want 0.045", got.CostUSD)
	}
	if *got.InputTokens != 999 || *got.OutputTokens != 888 ||
		*got.CacheReadTokens != 777 || *got.CacheWriteTokens != 666 {
		t.Fatalf("ResultMessage usage was not preferred: %+v", got)
	}
}

func TestExtractClaudeUsage_ResultCostOnly_NoTokens(t *testing.T) {
	// ResultMessage with cost but no usage rollup, no assistant events with tokens —
	// only cost is exposed.
	msgs := []services.ClaudeMessage{newResult(0.123)}
	got := extractClaudeUsage(msgs)
	if got == nil {
		t.Fatal("expected non-nil usage")
	}
	if got.CostUSD == nil || *got.CostUSD != 0.123 {
		t.Fatalf("CostUSD = %v, want 0.123", got.CostUSD)
	}
	if got.InputTokens != nil || got.OutputTokens != nil {
		t.Fatalf("expected nil token counts, got input=%v output=%v", got.InputTokens, got.OutputTokens)
	}
}
