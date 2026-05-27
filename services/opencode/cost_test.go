package opencode

import (
	"context"
	"errors"
	"testing"
	"time"
)

func stubQuery(rows []byte, err error) dbQueryFunc {
	return func(_ context.Context, _ string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		return rows, nil
	}
}

func TestFetchOpenCodeUsage_NilForEmptySession(t *testing.T) {
	got := fetchOpenCodeUsage(context.Background(), "", time.Now(), stubQuery([]byte("[]"), nil))
	if got != nil {
		t.Fatalf("expected nil usage for empty session id, got %+v", got)
	}

	got = fetchOpenCodeUsage(context.Background(), "unknown", time.Now(), stubQuery([]byte("[]"), nil))
	if got != nil {
		t.Fatalf("expected nil usage for placeholder 'unknown' session id, got %+v", got)
	}
}

func TestFetchOpenCodeUsage_SuspiciousSessionIDRejected(t *testing.T) {
	got := fetchOpenCodeUsage(
		context.Background(),
		"ses_bad'; DROP TABLE message; --",
		time.Now(),
		stubQuery([]byte("[]"), nil),
	)
	if got != nil {
		t.Fatalf("expected nil usage for suspicious session id, got %+v", got)
	}
}

func TestFetchOpenCodeUsage_QueryErrorReturnsNil(t *testing.T) {
	got := fetchOpenCodeUsage(
		context.Background(),
		"ses_1",
		time.Now(),
		stubQuery(nil, errors.New("db locked")),
	)
	if got != nil {
		t.Fatalf("expected nil usage when DB query fails, got %+v", got)
	}
}

func TestFetchOpenCodeUsage_EmptyRowsReturnsNil(t *testing.T) {
	got := fetchOpenCodeUsage(context.Background(), "ses_1", time.Now(), stubQuery([]byte("[]"), nil))
	if got != nil {
		t.Fatalf("expected nil usage for zero matching rows, got %+v", got)
	}
}

func TestFetchOpenCodeUsage_SingleRow(t *testing.T) {
	row := []byte(`[
		{
			"cost": 0.0283605,
			"tokens": "{\"total\":15902,\"input\":15800,\"output\":102,\"reasoning\":0,\"cache\":{\"write\":12,\"read\":3000}}",
			"modelID": "gpt-5.3-codex"
		}
	]`)
	got := fetchOpenCodeUsage(context.Background(), "ses_1", time.Now(), stubQuery(row, nil))
	if got == nil {
		t.Fatal("expected non-nil usage")
	}
	if got.CostUSD == nil || *got.CostUSD != 0.0283605 {
		t.Fatalf("CostUSD = %v, want 0.0283605", got.CostUSD)
	}
	if got.InputTokens == nil || *got.InputTokens != 15800 {
		t.Fatalf("InputTokens = %v, want 15800", got.InputTokens)
	}
	if got.OutputTokens == nil || *got.OutputTokens != 102 {
		t.Fatalf("OutputTokens = %v, want 102", got.OutputTokens)
	}
	if got.CacheReadTokens == nil || *got.CacheReadTokens != 3000 {
		t.Fatalf("CacheReadTokens = %v, want 3000", got.CacheReadTokens)
	}
	if got.CacheWriteTokens == nil || *got.CacheWriteTokens != 12 {
		t.Fatalf("CacheWriteTokens = %v, want 12", got.CacheWriteTokens)
	}
	if got.Model == nil || *got.Model != "gpt-5.3-codex" {
		t.Fatalf("Model = %v, want gpt-5.3-codex", got.Model)
	}
}

func TestFetchOpenCodeUsage_MultiRowSumsCorrectly(t *testing.T) {
	// Two assistant messages within the same `opencode run` (model thinks → tool → finalizes).
	// Cost and tokens should sum; model is taken from the latest row.
	row := []byte(`[
		{
			"cost": 0.01,
			"tokens": "{\"total\":1000,\"input\":900,\"output\":100,\"cache\":{\"write\":0,\"read\":50}}",
			"modelID": "kimi-k2.5"
		},
		{
			"cost": 0.02,
			"tokens": "{\"total\":2000,\"input\":1800,\"output\":200,\"cache\":{\"write\":10,\"read\":150}}",
			"modelID": "kimi-k2.6"
		}
	]`)
	got := fetchOpenCodeUsage(context.Background(), "ses_2", time.Now(), stubQuery(row, nil))
	if got == nil {
		t.Fatal("expected non-nil usage for multi-row session")
	}
	if *got.CostUSD != 0.03 {
		t.Fatalf("CostUSD sum = %v, want 0.03", *got.CostUSD)
	}
	if *got.InputTokens != 2700 {
		t.Fatalf("InputTokens sum = %v, want 2700", *got.InputTokens)
	}
	if *got.OutputTokens != 300 {
		t.Fatalf("OutputTokens sum = %v, want 300", *got.OutputTokens)
	}
	if *got.CacheReadTokens != 200 {
		t.Fatalf("CacheReadTokens sum = %v, want 200", *got.CacheReadTokens)
	}
	if *got.CacheWriteTokens != 10 {
		t.Fatalf("CacheWriteTokens sum = %v, want 10", *got.CacheWriteTokens)
	}
	if got.Model == nil || *got.Model != "kimi-k2.6" {
		t.Fatalf("Model = %v, want kimi-k2.6 (latest row)", got.Model)
	}
}

func TestFetchOpenCodeUsage_FreeModelZeroCost(t *testing.T) {
	// Free OpenCode models report cost=0 but still report tokens; we should
	// propagate both rather than dropping the row.
	row := []byte(`[
		{
			"cost": 0,
			"tokens": "{\"total\":24274,\"input\":24258,\"output\":3,\"cache\":{\"write\":0,\"read\":0}}",
			"modelID": "deepseek-v4-flash-free"
		}
	]`)
	got := fetchOpenCodeUsage(context.Background(), "ses_3", time.Now(), stubQuery(row, nil))
	if got == nil {
		t.Fatal("expected non-nil usage for free model run")
	}
	if got.CostUSD == nil || *got.CostUSD != 0 {
		t.Fatalf("CostUSD = %v, want 0", got.CostUSD)
	}
	if *got.InputTokens != 24258 {
		t.Fatalf("InputTokens = %v, want 24258", *got.InputTokens)
	}
}
