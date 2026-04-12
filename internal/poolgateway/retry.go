package poolgateway

import (
	"context"
	"time"
)

type RetryPolicy struct {
	MaxAttempts int
	BaseBackoff time.Duration
}

func defaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseBackoff: 200 * time.Millisecond,
	}
}

func (policy RetryPolicy) normalized() RetryPolicy {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = defaultRetryPolicy().MaxAttempts
	}
	if policy.BaseBackoff < 0 {
		policy.BaseBackoff = 0
	}
	return policy
}

func (policy RetryPolicy) shouldRetry(attempt int, gatewayError GatewayError) bool {
	normalized := policy.normalized()
	return attempt < normalized.MaxAttempts && gatewayError.Retryable
}

func (policy RetryPolicy) backoffFor(attempt int) time.Duration {
	normalized := policy.normalized()
	if attempt <= 0 || normalized.BaseBackoff == 0 {
		return 0
	}
	return time.Duration(attempt) * normalized.BaseBackoff
}

func waitWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func countsTowardOffline(gatewayError GatewayError) bool {
	switch gatewayError.Code {
	case "provider_unreachable", "provider_timeout":
		return true
	case "provider_http_error":
		return gatewayError.Retryable
	default:
		return false
	}
}
