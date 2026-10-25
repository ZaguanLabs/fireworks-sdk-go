package sdk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
)

type SSEProtocolError struct{ Message string }

func (e *SSEProtocolError) Error() string { return e.Message }

// UpstreamStreamError distinguishes an inference failure from its HTTP envelope.
type UpstreamStreamError struct {
	Status       int
	Code         string
	Type         string
	FinishReason string
	ResponseID   string
}

func (e *UpstreamStreamError) Error() string {
	return "upstream inference reported an unsuccessful completion stream"
}

type CompletionResponseError struct {
	Err                error
	StatusCode         int
	Headers            http.Header
	UpstreamResponseID string
}

func (e *CompletionResponseError) Error() string { return e.Err.Error() }
func (e *CompletionResponseError) Unwrap() error { return e.Err }

var upstreamTagPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func upstreamTag(value any) string {
	text := ""
	switch v := value.(type) {
	case string:
		text = v
	case int:
		text = fmt.Sprint(v)
	case float64:
		if v == float64(int(v)) {
			text = fmt.Sprint(int(v))
		}
	}
	if upstreamTagPattern.MatchString(text) {
		return text
	}
	return ""
}
func upstreamStreamError(payload map[string]any, reason, id string) *UpstreamStreamError {
	e := &UpstreamStreamError{FinishReason: upstreamTag(reason), ResponseID: boundedCorrelation(id)}
	if errorData, ok := payload["error"].(map[string]any); ok {
		for _, key := range []string{"status_code", "status"} {
			if n, ok := intFromStrictAny(errorData[key]); ok && n >= 400 && n <= 599 {
				e.Status = n
				break
			}
		}
		e.Code = upstreamTag(errorData["code"])
		e.Type = upstreamTag(errorData["type"])
	}
	return e
}

func boundedCorrelation(value string) string {
	chars := []rune(value)
	if len(chars) > 256 {
		chars = chars[:256]
	}
	result := ""
	for _, ch := range chars {
		if ch < 32 || ch == 127 {
			result += fmt.Sprintf("\\x%02x", ch)
		} else {
			result += string(ch)
		}
	}
	return result
}

func decodeStreamEvent(data string) (map[string]any, error) {
	var value map[string]any
	if err := json.Unmarshal([]byte(data), &value); err != nil || value == nil {
		return nil, &SSEProtocolError{Message: "completion stream event must be a JSON object"}
	}
	return value, nil
}
