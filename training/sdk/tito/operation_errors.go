package tito

import (
	"context"
	"errors"
	"net"

	sdk "github.com/ZaguanLabs/fireworks-sdk-go/training/sdk"
)

func operationError(cause error, phase string) *Error {
	var typed *Error
	if errors.As(cause, &typed) {
		return typed
	}
	contextData := map[string]any{"phase": phase, "transport_hop": "sidecar_to_inference"}
	var sampled *sdk.SamplingRequestError
	if errors.As(cause, &sampled) {
		contextData["logical_request_id"] = sampled.LogicalRequestID
		contextData["upstream_request_id"] = sampled.RequestID
		contextData["attempts"] = sampled.Attempts
		for key, value := range sampled.AsErrorRecord() {
			contextData[key] = value
		}
		delete(contextData, "model")
		delete(contextData, "context")
		delete(contextData, "source_error")
		if sampled.FinalErrorKind == "upstream_stream" {
			status := sampled.FinalStatus
			if status < 400 {
				status = 502
			}
			return &Error{Code: "tito_server_stream_error", Status: status, Message: "upstream inference reported a stream failure", Origin: "server", Context: contextData}
		}
		return &Error{Code: "tito_transport_error", Status: 502, Message: "inference request exhausted its retry budget", Origin: "transport", Context: contextData}
	}
	var protocol *sdk.SSEProtocolError
	if errors.As(cause, &protocol) {
		return &Error{Code: "tito_sdk_sampling_parse_error", Status: 422, Message: "local sampling parser rejected the inference stream; discard this trajectory", Origin: "sdk_parser", DiscardTrajectory: true, Context: contextData}
	}
	var status *sdk.CompletionHTTPStatusError
	if errors.As(cause, &status) {
		contextData["upstream_status"] = status.StatusCode
		contextData["wire_status"] = status.StatusCode
		contextData["upstream_request_id"] = status.RequestID()
		retry := status.StatusCode == 408 || status.StatusCode == 429 || status.StatusCode == 500 || status.StatusCode == 502 || status.StatusCode == 503 || status.StatusCode == 504
		return &Error{Code: "tito_server_http_error", Status: status.StatusCode, Message: "upstream inference returned an HTTP error", Origin: "server", ShouldRetry: retry, Context: contextData}
	}
	var transport net.Error
	if errors.As(cause, &transport) || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return &Error{Code: "tito_transport_error", Status: 502, Message: "upstream inference transport failed", Origin: "transport", ShouldRetry: true, Context: contextData}
	}
	return &Error{Code: "tito_sdk_internal_error", Status: 500, Message: "local TITO SDK operation failed", Origin: "sdk", DiscardTrajectory: true, Context: contextData}
}
