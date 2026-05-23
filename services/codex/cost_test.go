package codex

import (
	"math"
	"testing"
)

func turn(input, cached, output int) TurnCompletedMessage {
	var t TurnCompletedMessage
	t.Type = "turn.completed"
	t.Usage.InputTokens = input
	t.Usage.CachedInputTokens = cached
	t.Usage.OutputTokens = output
	return t
}

func TestExtractCodexUsage_NoTurnMessages(t *testing.T) {
	msgs := []CodexMessage{
		ThreadStartedMessage{Type: "thread.started", ThreadID: "th_1"},
		ItemCompletedMessage{Type: "item.completed"},
	}
	if got := extractCodexUsage(msgs, "gpt-5"); got != nil {
		t.Fatalf("expected nil usage when no turn.completed events present, got %+v", got)
	}
}

func TestExtractCodexUsage_SumsAcrossTurns(t *testing.T) {
	msgs := []CodexMessage{
		turn(1000, 500, 200),
		turn(300, 100, 80),
	}
	got := extractCodexUsage(msgs, "gpt-5-codex")
	if got == nil {
		t.Fatal("expected non-nil usage")
	}
	if *got.InputTokens != 1300 {
		t.Fatalf("InputTokens = %v, want 1300", *got.InputTokens)
	}
	if *got.OutputTokens != 280 {
		t.Fatalf("OutputTokens = %v, want 280", *got.OutputTokens)
	}
	if *got.CacheReadTokens != 600 {
		t.Fatalf("CacheReadTokens = %v, want 600", *got.CacheReadTokens)
	}
	if *got.CacheWriteTokens != 0 {
		t.Fatalf("CacheWriteTokens should always be 0 for Codex, got %v", *got.CacheWriteTokens)
	}
	if got.Model != "gpt-5-codex" {
		t.Fatalf("Model = %q, want gpt-5-codex", got.Model)
	}
	// Cost should be computable from gpt-5-codex pricing.
	// fresh = 1300 - 600 = 700 → 700/1M * 1.25 = 0.000875
	// cached = 600/1M * 0.125 = 0.000075
	// output = 280/1M * 10.00 = 0.0028
	// total = 0.00375
	const wantCost = 0.000875 + 0.000075 + 0.0028
	if got.CostUSD == nil || math.Abs(*got.CostUSD-wantCost) > 1e-9 {
		t.Fatalf("CostUSD = %v, want %v", got.CostUSD, wantCost)
	}
}

func TestExtractCodexUsage_UnknownModelLeavesCostNil(t *testing.T) {
	msgs := []CodexMessage{turn(100, 0, 50)}
	got := extractCodexUsage(msgs, "not-a-known-model")
	if got == nil {
		t.Fatal("expected non-nil usage for known token data")
	}
	if got.CostUSD != nil {
		t.Fatalf("expected nil CostUSD for unknown model, got %v", *got.CostUSD)
	}
	// Tokens should still be populated.
	if got.InputTokens == nil || *got.InputTokens != 100 {
		t.Fatalf("InputTokens = %v, want 100", got.InputTokens)
	}
}

func TestExtractCodexUsage_AllZeroTurnsIgnored(t *testing.T) {
	// Turn events with all-zero usage are noise (e.g. turn.started without usage)
	// and should not produce a usage record on their own.
	msgs := []CodexMessage{turn(0, 0, 0), turn(0, 0, 0)}
	if got := extractCodexUsage(msgs, "gpt-5"); got != nil {
		t.Fatalf("expected nil usage when all turns are zero, got %+v", got)
	}
}
