package handlers

import (
	"testing"

	"nairid/models"
	"nairid/services"
)

func ptrFloat(v float64) *float64 { return &v }
func ptrI64(v int64) *int64       { return &v }

func TestApplyUsageToAssistantPayload_NilUsageLeavesFieldsNil(t *testing.T) {
	p := &models.AssistantMessagePayload{
		JobID:   "job_1",
		Message: "hi",
	}
	applyUsageToAssistantPayload(p, nil)
	if p.CostUSD != nil || p.InputTokens != nil || p.OutputTokens != nil ||
		p.CacheReadTokens != nil || p.CacheWriteTokens != nil || p.Model != nil {
		t.Fatalf("expected all cost fields nil for nil usage, got %+v", p)
	}
}

func TestApplyUsageToAssistantPayload_NilPayloadIsNoOp(t *testing.T) {
	// Should not panic.
	applyUsageToAssistantPayload(nil, &services.CLIAgentUsage{CostUSD: ptrFloat(0.01)})
}

func TestApplyUsageToAssistantPayload_PopulatesAllFields(t *testing.T) {
	usage := &services.CLIAgentUsage{
		CostUSD:          ptrFloat(0.0283605),
		InputTokens:      ptrI64(21744),
		OutputTokens:     ptrI64(23),
		CacheReadTokens:  ptrI64(1792),
		CacheWriteTokens: ptrI64(0),
		Model:            "claude-sonnet-4-5-20250929",
	}
	p := &models.AssistantMessagePayload{JobID: "j", Message: "m"}
	applyUsageToAssistantPayload(p, usage)
	if p.CostUSD == nil || *p.CostUSD != 0.0283605 {
		t.Fatalf("CostUSD not propagated: %v", p.CostUSD)
	}
	if p.InputTokens == nil || *p.InputTokens != 21744 {
		t.Fatalf("InputTokens not propagated: %v", p.InputTokens)
	}
	if p.OutputTokens == nil || *p.OutputTokens != 23 {
		t.Fatalf("OutputTokens not propagated: %v", p.OutputTokens)
	}
	if p.CacheReadTokens == nil || *p.CacheReadTokens != 1792 {
		t.Fatalf("CacheReadTokens not propagated: %v", p.CacheReadTokens)
	}
	if p.CacheWriteTokens == nil || *p.CacheWriteTokens != 0 {
		t.Fatalf("CacheWriteTokens not propagated: %v", p.CacheWriteTokens)
	}
	if p.Model == nil || *p.Model != "claude-sonnet-4-5-20250929" {
		t.Fatalf("Model not propagated: %v", p.Model)
	}
}

func TestApplyUsageToAssistantPayload_EmptyModelStaysNil(t *testing.T) {
	// Usage with cost but no model name should not produce a non-nil Model
	// pointer to the empty string in the outgoing payload.
	usage := &services.CLIAgentUsage{CostUSD: ptrFloat(0.05)}
	p := &models.AssistantMessagePayload{}
	applyUsageToAssistantPayload(p, usage)
	if p.Model != nil {
		t.Fatalf("expected nil Model for empty usage.Model, got %v", p.Model)
	}
	if p.CostUSD == nil || *p.CostUSD != 0.05 {
		t.Fatalf("CostUSD not propagated: %v", p.CostUSD)
	}
}
