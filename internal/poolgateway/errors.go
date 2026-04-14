package poolgateway

import "errors"

var (
	ErrNoCapacity  = errors.New("no available API")
	ErrGlobalQuota = errors.New("global quota reached")
)

type InvocationFailedError struct {
	Response InvokeResponse
}

func (err *InvocationFailedError) Error() string {
	if err.Response.Error != nil {
		return err.Response.Error.Message
	}
	return "invocation failed"
}

type ProviderInvocationError struct {
	GatewayError GatewayError
}

func (err *ProviderInvocationError) Error() string {
	return err.GatewayError.Message
}
