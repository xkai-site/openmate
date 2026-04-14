package poolgateway

import "testing"

func TestCalculateCostUsesInputCachedAndReasoningRates(t *testing.T) {
	t.Parallel()
	inputTokens := 10
	outputTokens := 20
	totalTokens := 30
	cachedInputTokens := 4
	reasoningTokens := 5
	inputRate := 1.0
	cachedRate := 0.25
	outputRate := 2.0
	reasoningRate := 3.0

	cost := calculateCost(&UsageMetrics{
		InputTokens:       &inputTokens,
		OutputTokens:      &outputTokens,
		TotalTokens:       &totalTokens,
		CachedInputTokens: &cachedInputTokens,
		ReasoningTokens:   &reasoningTokens,
	}, &PricingConfig{
		InputPer1MUSD:       &inputRate,
		CachedInputPer1MUSD: &cachedRate,
		OutputPer1MUSD:      &outputRate,
		ReasoningPer1MUSD:   &reasoningRate,
	})
	if cost == nil {
		t.Fatalf("expected cost")
	}

	expected := (6*1.0 + 4*0.25 + 15*2.0 + 5*3.0) / 1_000_000
	if *cost != expected {
		t.Fatalf("unexpected cost: got=%f want=%f", *cost, expected)
	}
}
