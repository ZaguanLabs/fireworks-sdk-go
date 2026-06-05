package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

type APIError struct {
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

type BadRequestError struct{ *APIError }
type AuthenticationError struct{ *APIError }
type PermissionDeniedError struct{ *APIError }
type NotFoundError struct{ *APIError }
type ConflictError struct{ *APIError }
type UnprocessableEntityError struct{ *APIError }
type RateLimitError struct{ *APIError }
type InternalServerError struct{ *APIError }

func statusError(resp *http.Response, body []byte) error {
	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       body,
		BodyJSON:   decodeErrorBody(body),
		Header:     resp.Header.Clone(),
	}
	switch resp.StatusCode {
	case http.StatusBadRequest:
		return &BadRequestError{apiErr}
	case http.StatusUnauthorized:
		return &AuthenticationError{apiErr}
	case http.StatusForbidden:
		return &PermissionDeniedError{apiErr}
	case http.StatusNotFound:
		return &NotFoundError{apiErr}
	case http.StatusConflict:
		return &ConflictError{apiErr}
	case http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{apiErr}
	case http.StatusTooManyRequests:
		return &RateLimitError{apiErr}
	default:
		if resp.StatusCode >= 500 {
			return &InternalServerError{apiErr}
		}
		return apiErr
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
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(ctxErr, context.DeadlineExceeded)
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
