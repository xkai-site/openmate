package poolgateway

func summarizeUsage(records []InvocationRecord, nodeID *string, limit *int) UsageSummary {
	summary := UsageSummary{
		NodeID:      copyStringPointer(nodeID),
		Limit:       copyIntPointer(limit),
		GeneratedAt: utcNow(),
	}

	totalLatency := 0
	latencyCount := 0
	var maxLatency *int
	totalCost := 0.0
	hasCost := false

	for _, record := range records {
		summary.InvocationCount++
		switch record.Status {
		case InvocationStatusSuccess:
			summary.SuccessCount++
		case InvocationStatusFailure:
			summary.FailureCount++
		}

		summary.AttemptCount += len(record.Attempts)
		if len(record.Attempts) > 1 {
			summary.RetryCount += len(record.Attempts) - 1
		}

		if record.Usage != nil {
			summary.PromptTokens += intOrZero(record.Usage.PromptTokens)
			summary.CompletionTokens += intOrZero(record.Usage.CompletionTokens)
			summary.TotalTokens += intOrZero(record.Usage.TotalTokens)
			if record.Usage.CostUSD != nil {
				totalCost += *record.Usage.CostUSD
				hasCost = true
			}
		}

		if record.Timing.LatencyMS != nil {
			latency := *record.Timing.LatencyMS
			totalLatency += latency
			latencyCount++
			if maxLatency == nil || latency > *maxLatency {
				value := latency
				maxLatency = &value
			}
		}
	}

	if latencyCount > 0 {
		average := totalLatency / latencyCount
		summary.AvgLatencyMS = &average
	}
	if maxLatency != nil {
		value := *maxLatency
		summary.MaxLatencyMS = &value
	}
	if hasCost {
		value := totalCost
		summary.TotalCostUSD = &value
	}

	return summary
}

func intOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func copyStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}

func copyIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
