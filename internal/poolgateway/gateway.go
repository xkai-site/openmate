package poolgateway

import (
	"context"
	"errors"
	"time"
)

type ProviderFactory func(provider string) (ProviderClient, error)

type Gateway struct {
	store           *Store
	modelConfigPath string
	providerFactory ProviderFactory
	retryPolicy     *RetryPolicy
	sleepFn         func(context.Context, time.Duration) error
}

func NewGateway(store *Store, modelConfigPath string) *Gateway {
	return &Gateway{
		store:           store,
		modelConfigPath: modelConfigPath,
		providerFactory: GetProviderClient,
		sleepFn:         waitWithContext,
	}
}

func (gateway *Gateway) SetProviderFactory(factory ProviderFactory) {
	gateway.providerFactory = factory
}

func (gateway *Gateway) SetRetryPolicy(policy RetryPolicy) {
	normalized := policy.normalized()
	gateway.retryPolicy = &normalized
}

func (gateway *Gateway) SetSleepFn(sleepFn func(context.Context, time.Duration) error) {
	if sleepFn == nil {
		gateway.sleepFn = waitWithContext
		return
	}
	gateway.sleepFn = sleepFn
}

func (gateway *Gateway) Invoke(ctx context.Context, request InvokeRequest) (InvokeResponse, error) {
	config, err := LoadModelConfig(gateway.modelConfigPath)
	if err != nil {
		return InvokeResponse{}, err
	}
	request = normalizeRequest(request)
	reservation, err := gateway.store.ReserveInvocation(ctx, config, request)
	if err != nil {
		return InvokeResponse{}, err
	}
	return gateway.invokeReserved(ctx, config, request, reservation)
}

func (gateway *Gateway) invokeReserved(
	ctx context.Context,
	config ModelConfig,
	request InvokeRequest,
	reservation InvocationReservation,
) (InvokeResponse, error) {
	policy := gateway.resolveRetryPolicy(config)
	currentReservation := reservation
	for attempt := 1; ; attempt++ {
		provider, err := gateway.providerFactory(currentReservation.Provider)
		if err != nil {
			return gateway.finishInternalFailure(ctx, currentReservation, err)
		}

		providerResult, err := provider.Invoke(ctx, currentReservation, request)
		if err != nil {
			providerErr, ok := err.(*ProviderInvocationError)
			if !ok {
				return gateway.finishInternalFailure(ctx, currentReservation, err)
			}

			finishedAt := utcNow()
			if err := gateway.store.CompleteAttemptFailure(ctx, currentReservation, providerErr.GatewayError, finishedAt); err != nil {
				return InvokeResponse{}, err
			}
			if !policy.shouldRetry(attempt, providerErr.GatewayError) {
				return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, providerErr.GatewayError, finishedAt)
			}
			if err := gateway.sleepFn(ctx, policy.backoffFor(attempt)); err != nil {
				return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, internalGatewayError(err), utcNow())
			}

			nextReservation, reserveErr := gateway.store.ReserveRetryAttempt(ctx, config, currentReservation.InvocationID, request)
			if reserveErr != nil {
				if errors.Is(reserveErr, ErrNoCapacity) || errors.Is(reserveErr, ErrGlobalQuota) {
					return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, providerErr.GatewayError, utcNow())
				}
				return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, internalGatewayError(reserveErr), utcNow())
			}
			currentReservation = nextReservation
			continue
		}

		finishedAt := utcNow()
		latencyMS := int(finishedAt.Sub(currentReservation.StartedAt).Milliseconds())
		usage := withLatency(providerResult.Usage, latencyMS)
		return gateway.store.CompleteInvocationSuccess(
			ctx,
			currentReservation,
			providerResult.OutputText,
			providerResult.RawResponse,
			usage,
			finishedAt,
		)
	}
}

func (gateway *Gateway) resolveRetryPolicy(config ModelConfig) RetryPolicy {
	if gateway.retryPolicy != nil {
		return gateway.retryPolicy.normalized()
	}
	return config.RetryPolicy()
}

func (gateway *Gateway) Sync(ctx context.Context) (SyncResult, error) {
	config, err := LoadModelConfig(gateway.modelConfigPath)
	if err != nil {
		return SyncResult{}, err
	}
	if err := gateway.store.SyncFromModelConfig(ctx, config); err != nil {
		return SyncResult{}, err
	}
	capacity, err := gateway.store.Capacity(ctx, config)
	if err != nil {
		return SyncResult{}, err
	}
	return SyncResult{
		Synced:   true,
		Capacity: capacity,
	}, nil
}

func (gateway *Gateway) Capacity(ctx context.Context) (CapacitySnapshot, error) {
	config, err := LoadModelConfig(gateway.modelConfigPath)
	if err != nil {
		return CapacitySnapshot{}, err
	}
	return gateway.store.Capacity(ctx, config)
}

func (gateway *Gateway) Records(ctx context.Context, nodeID *string, limit *int) ([]InvocationRecord, error) {
	config, err := LoadModelConfig(gateway.modelConfigPath)
	if err != nil {
		return nil, err
	}
	return gateway.store.ListRecords(ctx, config, nodeID, limit)
}

func (gateway *Gateway) Usage(ctx context.Context, nodeID *string, limit *int) (UsageSummary, error) {
	config, err := LoadModelConfig(gateway.modelConfigPath)
	if err != nil {
		return UsageSummary{}, err
	}
	records, err := gateway.store.ListRecords(ctx, config, nodeID, limit)
	if err != nil {
		return UsageSummary{}, err
	}
	return summarizeUsage(records, nodeID, limit), nil
}

func withLatency(usage *UsageMetrics, latencyMS int) *UsageMetrics {
	if usage == nil {
		return &UsageMetrics{
			LatencyMS: &latencyMS,
		}
	}
	result := *usage
	result.LatencyMS = &latencyMS
	return &result
}

func utcNow() time.Time {
	return time.Now().UTC()
}

func normalizeRequest(request InvokeRequest) InvokeRequest {
	if request.ResponseMode == "" {
		request.ResponseMode = ResponseModeText
	}
	if request.Metadata == nil {
		request.Metadata = map[string]any{}
	}
	if request.Messages == nil {
		request.Messages = []LlmMessage{}
	}
	return request
}

func (gateway *Gateway) finishInternalFailure(
	ctx context.Context,
	reservation InvocationReservation,
	err error,
) (InvokeResponse, error) {
	gatewayError := internalGatewayError(err)
	finishedAt := utcNow()
	if err := gateway.store.CompleteAttemptFailure(ctx, reservation, gatewayError, finishedAt); err != nil {
		return InvokeResponse{}, err
	}
	return gateway.finishInvocationFailure(ctx, reservation.InvocationID, gatewayError, finishedAt)
}

func (gateway *Gateway) finishInvocationFailure(
	ctx context.Context,
	invocationID string,
	gatewayError GatewayError,
	finishedAt time.Time,
) (InvokeResponse, error) {
	response, err := gateway.store.CompleteInvocationFailure(ctx, invocationID, gatewayError, finishedAt)
	if err != nil {
		return InvokeResponse{}, err
	}
	return response, &InvocationFailedError{Response: response}
}

func internalGatewayError(err error) GatewayError {
	exceptionType := "internal_error"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		exceptionType = "context_deadline_exceeded"
	case errors.Is(err, context.Canceled):
		exceptionType = "context_canceled"
	}
	return GatewayError{
		Code:      "gateway_internal_error",
		Message:   err.Error(),
		Retryable: false,
		Details: map[string]any{
			"exception_type": exceptionType,
		},
	}
}
