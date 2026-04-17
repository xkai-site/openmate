package poolgateway

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"vos/internal/openmate/observability"
)

type ProviderFactory func(provider string) (ProviderClient, error)

type Gateway struct {
	store           *Store
	modelConfigPath string
	providerFactory ProviderFactory
	retryPolicy     *RetryPolicy
	sleepFn         func(context.Context, time.Duration) error
	logger          *slog.Logger
}

func NewGateway(store *Store, modelConfigPath string) *Gateway {
	return &Gateway{
		store:           store,
		modelConfigPath: modelConfigPath,
		providerFactory: GetProviderClient,
		sleepFn:         waitWithContext,
		logger:          observability.NormalizeLogger(nil),
	}
}

func (gateway *Gateway) SetLogger(logger *slog.Logger) {
	gateway.logger = observability.NormalizeLogger(logger)
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
	logger := observability.LoggerFromContext(ctx, gateway.logger).With(
		slog.String(observability.FieldOperation, "pool.invoke"),
		slog.String(observability.FieldRequestID, request.RequestID),
		slog.String(observability.FieldNodeID, request.NodeID),
	)
	invokedAt := utcNow()
	config, err := LoadModelConfig(gateway.modelConfigPath)
	if err != nil {
		logger.Error("load model config failed", slog.Any("error", err))
		return InvokeResponse{}, err
	}
	request = normalizeRequest(request)
	if err := validateInvokeRequest(request); err != nil {
		logger.Error("validate invoke request failed", slog.Any("error", err))
		return InvokeResponse{}, err
	}
	reservation, err := gateway.store.ReserveInvocation(ctx, config, request)
	if err != nil {
		logger.Error("reserve invocation failed", slog.Any("error", err))
		return InvokeResponse{}, err
	}
	logger = logger.With(
		slog.String(observability.FieldInvocationID, reservation.InvocationID),
		slog.String(observability.FieldAttemptID, reservation.AttemptID),
	)
	ctx = observability.WithLogger(ctx, logger)
	response, err := gateway.invokeReserved(ctx, config, request, reservation)
	if err != nil {
		logger.Error("invoke failed", slog.Any("error", err))
		return response, err
	}
	logger.Info(
		"invoke succeeded",
		slog.String("status", string(response.Status)),
		slog.Int64(observability.FieldDurationMS, int64(utcNow().Sub(invokedAt).Milliseconds())),
	)
	return response, nil
}

func (gateway *Gateway) invokeReserved(
	ctx context.Context,
	config ModelConfig,
	request InvokeRequest,
	reservation InvocationReservation,
) (InvokeResponse, error) {
	logger := observability.LoggerFromContext(ctx, gateway.logger).With(
		slog.String(observability.FieldOperation, "pool.invoke_reserved"),
		slog.String(observability.FieldInvocationID, reservation.InvocationID),
		slog.String(observability.FieldAttemptID, reservation.AttemptID),
	)
	policy := gateway.resolveRetryPolicy(config)
	currentReservation := reservation
	for attempt := 1; ; attempt++ {
		attemptLogger := logger.With(
			slog.Int("attempt", attempt),
			slog.String(observability.FieldAttemptID, currentReservation.AttemptID),
		)
		attemptLogger.Debug(
			"attempt started",
			slog.String("provider", currentReservation.Provider),
			slog.String("api_id", currentReservation.APIID),
		)
		provider, err := gateway.providerFactory(currentReservation.Provider)
		if err != nil {
			attemptLogger.Error("provider factory failed", slog.Any("error", err))
			return gateway.finishInternalFailure(ctx, currentReservation, err)
		}

		providerResult, err := provider.Invoke(ctx, currentReservation, request)
		if err != nil {
			providerErr, ok := err.(*ProviderInvocationError)
			if !ok {
				attemptLogger.Error("provider invoke failed with non-provider error", slog.Any("error", err))
				return gateway.finishInternalFailure(ctx, currentReservation, err)
			}

			finishedAt := utcNow()
			if err := gateway.store.CompleteAttemptFailure(ctx, currentReservation, providerErr.GatewayError, finishedAt); err != nil {
				attemptLogger.Error("complete attempt failure failed", slog.Any("error", err))
				return InvokeResponse{}, err
			}
			attemptLogger.Warn(
				"attempt failed",
				slog.String("error_code", providerErr.GatewayError.Code),
				slog.Bool("retryable", providerErr.GatewayError.Retryable),
			)
			if !policy.shouldRetry(attempt, providerErr.GatewayError) {
				return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, providerErr.GatewayError, finishedAt)
			}
			if err := gateway.sleepFn(ctx, policy.backoffFor(attempt)); err != nil {
				attemptLogger.Error("retry backoff interrupted", slog.Any("error", err))
				return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, internalGatewayError(err), utcNow())
			}

			nextReservation, reserveErr := gateway.store.ReserveRetryAttempt(ctx, config, currentReservation.InvocationID, request)
			if reserveErr != nil {
				attemptLogger.Error("reserve retry attempt failed", slog.Any("error", reserveErr))
				if errors.Is(reserveErr, ErrNoCapacity) || errors.Is(reserveErr, ErrGlobalQuota) {
					return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, providerErr.GatewayError, utcNow())
				}
				return gateway.finishInvocationFailure(ctx, currentReservation.InvocationID, internalGatewayError(reserveErr), utcNow())
			}
			currentReservation = nextReservation
			attemptLogger.Debug(
				"retry reserved",
				slog.String(observability.FieldAttemptID, currentReservation.AttemptID),
			)
			continue
		}

		finishedAt := utcNow()
		latencyMS := int(finishedAt.Sub(currentReservation.StartedAt).Milliseconds())
		usage := finalizeUsage(providerResult.Usage, latencyMS, currentReservation.Pricing)
		attemptLogger.Info(
			"attempt succeeded",
			slog.Int("latency_ms", latencyMS),
		)
		return gateway.store.CompleteInvocationSuccess(
			ctx,
			currentReservation,
			providerResult.OutputText,
			providerResult.Response,
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

func utcNow() time.Time {
	return time.Now().UTC()
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
