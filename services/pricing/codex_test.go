package pricing

import (
	"math"
	"testing"
)

func TestCodexCostUSD_KnownModels(t *testing.T) {
	tests := []struct {
		name              string
		model             string
		inputTokens       int64
		cachedInputTokens int64
		outputTokens      int64
		want              float64
	}{
		{
			name:              "gpt-5 single turn no cache",
			model:             "gpt-5",
			inputTokens:       1_000_000,
			cachedInputTokens: 0,
			outputTokens:      100_000,
			// 1M input * $1.25 + 0.1M output * $10.00 = 1.25 + 1.00 = 2.25
			want: 2.25,
		},
		{
			name:              "gpt-5-codex with cached inputs",
			model:             "gpt-5-codex",
			inputTokens:       1_000_000,
			cachedInputTokens: 800_000, // fresh = 200_000
			outputTokens:      50_000,
			// fresh 0.2M * $1.25 + cached 0.8M * $0.125 + output 0.05M * $10.00
			// = 0.25 + 0.10 + 0.50 = 0.85
			want: 0.85,
		},
		{
			name:              "gpt-5-mini small turn",
			model:             "gpt-5-mini",
			inputTokens:       10_000,
			cachedInputTokens: 0,
			outputTokens:      1_000,
			// 10k * 0.25/1M + 1k * 2.00/1M = 0.0025 + 0.002 = 0.0045
			want: 0.0045,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CodexCostUSD(tt.model, tt.inputTokens, tt.cachedInputTokens, tt.outputTokens)
			if !ok {
				t.Fatalf("expected ok=true for known model %q", tt.model)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("CodexCostUSD(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestCodexCostUSD_SnapshotModelMatches(t *testing.T) {
	// Snapshot-suffixed id should fall back to its base model pricing.
	got, ok := CodexCostUSD("gpt-5-codex-2026-04-01", 1_000_000, 0, 0)
	if !ok {
		t.Fatalf("expected snapshot model to match gpt-5-codex base pricing")
	}
	const want = 1.25 // 1M input tokens * $1.25/M
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("snapshot match cost = %v, want %v", got, want)
	}
}

func TestCodexCostUSD_PrefersLongerPrefix(t *testing.T) {
	// "gpt-5-mini-codex" should match its own entry, not the shorter "gpt-5" prefix.
	got, ok := CodexCostUSD("gpt-5-mini-codex-2026-05-01", 1_000_000, 0, 0)
	if !ok {
		t.Fatalf("expected match for snapshot of gpt-5-mini-codex")
	}
	const want = 0.25 // gpt-5-mini-codex input price per 1M
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("longer-prefix match cost = %v, want %v", got, want)
	}
}

func TestCodexCostUSD_UnknownModel(t *testing.T) {
	_, ok := CodexCostUSD("some-unknown-model", 100, 0, 100)
	if ok {
		t.Fatalf("expected ok=false for unknown model")
	}
}

func TestCodexCostUSD_DefensiveNegatives(t *testing.T) {
	// Negative inputs are clamped to zero, not propagated.
	got, ok := CodexCostUSD("gpt-5", -5, -5, -5)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got != 0 {
		t.Fatalf("expected zero cost for all-negative inputs, got %v", got)
	}
}

func TestCodexCostUSD_CachedExceedsInputClamps(t *testing.T) {
	// If cached > input the fresh count is clamped to zero so we never bill negative
	// fresh tokens. cached itself is still billed (the reported value is the source of truth).
	got, ok := CodexCostUSD("gpt-5", 100, 1_000, 0)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	// fresh = 0 (clamped), cached = 1000 * 0.125/1M = 0.000125
	const want = 0.000125
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("cached-clamp cost = %v, want %v", got, want)
	}
}
