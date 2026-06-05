package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	DocsSDK    = "https://docs.fireworks.ai/fine-tuning/training-sdk/introduction"
	DiscordURL = "https://discord.gg/mMqQxvFD9A"
	ConsoleURL = "https://app.fireworks.ai/account/home"

	AgentDebugInstructions = "First search the Fireworks training cookbook skill for this error. If the cookbook is not available locally, clone https://github.com/fw-ai/cookbook and search cookbook/skills."
	TrainingMaxWaitTime    = 5 * time.Minute
)

var HTTPStatusHints = map[int]string{
	http.StatusBadRequest:          "Check that all request parameters are valid.",
	http.StatusUnauthorized:        "Check your API key. Training APIs require a training-scoped Fireworks key; inference-only keys return 401. Manage keys at " + ConsoleURL,
	http.StatusForbidden:           "Your key is valid, but it may not have permission for this account or resource.",
	http.StatusNotFound:            "The resource was not found. Verify the ID/name is correct and belongs to the resolved account.",
	http.StatusConflict:            "Resource conflict. It may already exist or be in a transitional state.",
	http.StatusTooManyRequests:     "Rate limited. Wait and retry, or reach out on Discord: " + DiscordURL,
	http.StatusInternalServerError: "Internal server error. Try again. If persistent, reach out on Discord: " + DiscordURL,
	http.StatusServiceUnavailable:  "Service temporarily unavailable. Retry after a short wait.",
}

var RetryableStatusCodes = map[int]bool{
	http.StatusRequestTimeout:      true,
	http.StatusTooManyRequests:     true,
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
}

type SDKErrorFormatOptions struct {
	DocsURL     string
	ShowSupport bool
}

func FormatSDKError(what, cause, solution string, opts ...SDKErrorFormatOptions) string {
	var opt SDKErrorFormatOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	lines := []string{
		"ERROR: " + what,
		"  Cause: " + cause,
		"  Solution: " + solution,
		"  Agent debug: " + AgentDebugInstructions,
	}
	if opt.DocsURL != "" {
		lines = append(lines, "  Docs: "+opt.DocsURL)
	}
	if opt.ShowSupport {
		lines = append(lines, "  Support: "+DiscordURL)
	}
	return strings.Join(lines, "\n")
}

func ParseAPIError(resp *http.Response) string {
	if resp == nil {
		return ""
	}
	if resp.Body == nil {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		text := strings.TrimSpace(fmt.Sprint(resp))
		if len(text) > 200 {
			return text[:200]
		}
		return text
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return ParseAPIErrorBody(body)
}

func ParseAPIErrorBody(body []byte) string {
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err == nil {
		errValue, ok := decoded["error"]
		if !ok {
			return compactJSON(body)
		}
		switch value := errValue.(type) {
		case map[string]any:
			if message, ok := value["message"].(string); ok {
				return message
			}
			return compactJSONValue(value)
		case string:
			return value
		default:
			return fmt.Sprint(value)
		}
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		return text[:200]
	}
	return text
}

func IsRetryableStatusCode(statusCode int) bool {
	return RetryableStatusCodes[statusCode]
}

type RequestRetryOptions struct {
	MaxWaitTime time.Duration
	Now         func() time.Time
	Sleep       func(time.Duration)
}

func RequestWithRetries(fn func() (*http.Response, error), opts ...RequestRetryOptions) (*http.Response, error) {
	var opt RequestRetryOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	if opt.MaxWaitTime <= 0 {
		opt.MaxWaitTime = TrainingMaxWaitTime
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Sleep == nil {
		opt.Sleep = time.Sleep
	}

	start := opt.Now()
	attempt := 0
	for {
		resp, err := fn()
		if err != nil {
			if isRetryableRequestError(err) {
				if delay, ok := trainingBackoffDelay(attempt, start, opt); ok {
					attempt++
					opt.Sleep(delay)
					continue
				}
			}
			return nil, err
		}
		if resp != nil && IsRetryableStatusCode(resp.StatusCode) {
			if delay, ok := trainingBackoffDelay(attempt, start, opt); ok {
				attempt++
				opt.Sleep(delay)
				continue
			}
		}
		return resp, nil
	}
}

func trainingBackoffDelay(attempt int, start time.Time, opt RequestRetryOptions) (time.Duration, bool) {
	elapsed := opt.Now().Sub(start)
	if elapsed >= opt.MaxWaitTime {
		return 0, false
	}
	delay := time.Duration(1<<attempt) * time.Second
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	remaining := start.Add(opt.MaxWaitTime).Sub(opt.Now())
	if delay > remaining {
		delay = remaining
	}
	return delay, true
}

func isRetryableRequestError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func compactJSON(body []byte) string {
	var out bytes.Buffer
	if err := json.Compact(&out, body); err != nil {
		return strings.TrimSpace(string(body))
	}
	return out.String()
}

func compactJSONValue(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(payload)
}
