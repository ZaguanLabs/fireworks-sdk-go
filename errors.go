package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
)

type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

type APIError struct {
	Request    *http.Request
	StatusCode int
	Status     string
	Body       []byte
	BodyJSON   any
	Header     http.Header
}

func (e *APIError) Error() string {
	if len(e.Body) == 0 {
		return fmt.Sprintf("fireworks: API error: %s", e.Status)
	}
	return fmt.Sprintf("fireworks: API error: %s: %s", e.Status, string(e.Body))
}

func (e *APIError) IsStatus(status int) bool {
	return e != nil && e.StatusCode == status
}

type APIStatusError struct {
	*APIError
}

func (e *APIStatusError) As(target any) bool {
	return asAPIStatusError(e, target)
}

type APIConnectionError struct {
	Message string
	Request *http.Request
	Err     error
}

func (e *APIConnectionError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "fireworks: connection error"
}

func (e *APIConnectionError) Unwrap() error {
	return e.Err
}

type APITimeoutError struct {
	*APIConnectionError
}

type APIResponseValidationError struct {
	Message    string
	Request    *http.Request
	Response   *APIResponse
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
	Err        error
}

func (e *APIResponseValidationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "fireworks: response validation error"
}

func (e *APIResponseValidationError) Unwrap() error {
	return e.Err
}

func responseValidationError(resp *APIResponse, body []byte, err error) *APIResponseValidationError {
	out := &APIResponseValidationError{
		Message: "fireworks: response validation error",
		Body:    append([]byte(nil), body...),
		Err:     err,
	}
	if resp != nil {
		out.Request = resp.Request
		out.Response = resp
		out.StatusCode = resp.StatusCode
		out.Status = resp.Status
		out.Header = resp.Header.Clone()
	}
	return out
}

type BadRequestError struct{ *APIStatusError }
type AuthenticationError struct{ *APIStatusError }
type PermissionDeniedError struct{ *APIStatusError }
type NotFoundError struct{ *APIStatusError }
type ConflictError struct{ *APIStatusError }
type UnprocessableEntityError struct{ *APIStatusError }
type RateLimitError struct{ *APIStatusError }
type InternalServerError struct{ *APIStatusError }

func (e *BadRequestError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func (e *AuthenticationError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func (e *PermissionDeniedError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func (e *NotFoundError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func (e *ConflictError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func (e *UnprocessableEntityError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func (e *RateLimitError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func (e *InternalServerError) As(target any) bool {
	return asAPIStatusError(e.APIStatusError, target)
}

func asAPIStatusError(err *APIStatusError, target any) bool {
	if err == nil {
		return false
	}
	switch t := target.(type) {
	case **APIStatusError:
		*t = err
		return true
	case **APIError:
		*t = err.APIError
		return true
	default:
		return false
	}
}

func statusError(resp *http.Response, body []byte) error {
	apiErr := &APIError{
		Request:    resp.Request,
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       body,
		BodyJSON:   decodeErrorBody(body),
		Header:     resp.Header.Clone(),
	}
	statusErr := &APIStatusError{APIError: apiErr}
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return &BadRequestError{statusErr}
	case http.StatusUnauthorized:
		return &AuthenticationError{statusErr}
	case http.StatusForbidden:
		return &PermissionDeniedError{statusErr}
	case http.StatusNotFound:
		return &NotFoundError{statusErr}
	case http.StatusConflict:
		return &ConflictError{statusErr}
	case http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{statusErr}
	case http.StatusTooManyRequests:
		return &RateLimitError{statusErr}
	default:
		if resp.StatusCode >= 500 {
			return &InternalServerError{statusErr}
		}
		return statusErr
	}
}

func requestError(req *http.Request, err error) error {
	if err == nil {
		return nil
	}
	connErr := &APIConnectionError{
		Message: "fireworks: connection error",
		Request: req,
		Err:     err,
	}
	if req != nil && errorsIsTimeout(err, req.Context().Err()) {
		connErr.Message = "fireworks: request timed out"
		return &APITimeoutError{APIConnectionError: connErr}
	}
	if errorsIsTimeout(err, nil) {
		connErr.Message = "fireworks: request timed out"
		return &APITimeoutError{APIConnectionError: connErr}
	}
	return connErr
}

func errorsIsTimeout(err, ctxErr error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctxErr, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func decodeErrorBody(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil
	}
	return out
}
