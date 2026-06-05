package fireworks

import (
	"encoding/json"
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
