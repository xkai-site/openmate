package poolgateway

func finalizeUsage(usage *UsageMetrics, latencyMS int, pricing *PricingConfig) *UsageMetrics {
	if usage == nil {
		result := &UsageMetrics{
			LatencyMS: &latencyMS,
		}
		if cost := calculateCost(result, pricing); cost != nil {
			result.CostUSD = cost
		}
		return result
	}
	result := *usage
	result.LatencyMS = &latencyMS
	if cost := calculateCost(&result, pricing); cost != nil {
		result.CostUSD = cost
	}
	return &result
}

func calculateCost(usage *UsageMetrics, pricing *PricingConfig) *float64 {
	if usage == nil || pricing == nil {
		return nil
	}

	total := 0.0
	hasCost := false

	inputTokens := intOrZero(usage.InputTokens)
	cachedTokens := intOrZero(usage.CachedInputTokens)
	regularInputTokens := inputTokens - cachedTokens
	if regularInputTokens < 0 {
		regularInputTokens = 0
	}
	if pricing.InputPer1MUSD != nil && regularInputTokens > 0 {
		total += float64(regularInputTokens) * *pricing.InputPer1MUSD / 1_000_000
		hasCost = true
	}
	if pricing.CachedInputPer1MUSD != nil && cachedTokens > 0 {
		total += float64(cachedTokens) * *pricing.CachedInputPer1MUSD / 1_000_000
		hasCost = true
	}

	outputTokens := intOrZero(usage.OutputTokens)
	reasoningTokens := intOrZero(usage.ReasoningTokens)
	regularOutputTokens := outputTokens - reasoningTokens
	if regularOutputTokens < 0 {
		regularOutputTokens = 0
	}
	if pricing.OutputPer1MUSD != nil && regularOutputTokens > 0 {
		total += float64(regularOutputTokens) * *pricing.OutputPer1MUSD / 1_000_000
		hasCost = true
	}
	if reasoningTokens > 0 {
		switch {
		case pricing.ReasoningPer1MUSD != nil:
			total += float64(reasoningTokens) * *pricing.ReasoningPer1MUSD / 1_000_000
			hasCost = true
		case pricing.OutputPer1MUSD != nil:
			total += float64(reasoningTokens) * *pricing.OutputPer1MUSD / 1_000_000
			hasCost = true
		}
	}

	if !hasCost {
		return nil
	}
	value := total
	return &value
}
