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
	"sort"
	"strconv"
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
	http.StatusBadGateway:          "Bad gateway. Retry after a short wait.",
	http.StatusServiceUnavailable:  "Service temporarily unavailable. Retry after a short wait.",
	http.StatusGatewayTimeout:      "Gateway timeout. The request took too long upstream. Retry after a short wait.",
}

const errorInfoType = "type.googleapis.com/google.rpc.ErrorInfo"

var safeErrorInfoMetadataKeys = map[string]bool{
	"quota_required":  true,
	"quota_available": true,
}

type TrainingAPIError struct {
	Message    string
	StatusCode int
	Reason     string
	Metadata   map[string]string
}

func (e *TrainingAPIError) Error() string { return e.Message }

func ParseTrainingAPIError(resp *http.Response, contextLabel string) *TrainingAPIError {
	if resp == nil {
		return &TrainingAPIError{Message: contextLabel}
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewReader(body))
	message := ParseAPIErrorBody(body)
	reason, metadata := parseTrainingErrorInfo(body)
	if contextLabel != "" {
		message = fmt.Sprintf("%s (HTTP %d): %s", contextLabel, resp.StatusCode, message)
	}
	return &TrainingAPIError{
		Message:    message,
		StatusCode: resp.StatusCode,
		Reason:     reason,
		Metadata:   metadata,
	}
}

func parseTrainingErrorInfo(body []byte) (string, map[string]string) {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return "", nil
	}
	status := payload
	if nested, ok := payload["error"].(map[string]any); ok {
		status = nested
	}
	details, _ := status["details"].([]any)
	for _, item := range details {
		detail, _ := item.(map[string]any)
		if detail == nil || stringFromAny(detail["@type"]) != errorInfoType {
			continue
		}
		reason := strings.TrimSpace(stringFromAny(detail["reason"]))
		if reason == "" {
			continue
		}
		metadata := map[string]string{}
		if raw, ok := detail["metadata"].(map[string]any); ok {
			keys := make([]string, 0, len(safeErrorInfoMetadataKeys))
			for key := range safeErrorInfoMetadataKeys {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				value, ok := raw[key].(string)
				if !ok {
					continue
				}
				if len(value) > 128 {
					value = value[:128]
				}
				metadata[key] = value
			}
		}
		return reason, metadata
	}
	return "", nil
}

func FormatCheckpointPromotionError(resp *http.Response, checkpointID string) string {
	return formatPromotionError(resp, "Failed to promote checkpoint '"+checkpointID+"'", "Use a checkpoint name returned by list_checkpoints, ensure the row is promotable, and pass the base_model that matches the trainer.\n  Console: "+ConsoleURL)
}

func FormatSessionCheckpointPromotionError(resp *http.Response, checkpointID string) string {
	return formatPromotionError(resp, "Failed to promote session checkpoint '"+checkpointID+"'", "Use a checkpoint name returned by list_training_session_checkpoints, ensure the row is promotable, and pass the base_model that matches the trainer.\n  Console: "+ConsoleURL)
}

func formatPromotionError(resp *http.Response, what, clientSolution string) string {
	if resp == nil {
		return FormatSDKError(what, "empty response", clientSolution, SDKErrorFormatOptions{DocsURL: DocsSDK})
	}
	solution := clientSolution
	showSupport := false
	if resp.StatusCode >= 500 {
		solution = "Retry checkpoint promotion. If the error persists, contact Fireworks support."
		showSupport = true
	}
	return FormatSDKError(fmt.Sprintf("%s (HTTP %d)", what, resp.StatusCode), ParseAPIError(resp), solution, SDKErrorFormatOptions{DocsURL: DocsSDK, ShowSupport: showSupport})
}

func ParseRetryAfter(resp *http.Response, now ...time.Time) *time.Duration {
	if resp == nil {
		return nil
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
		if seconds < 0 {
			seconds = 0
		}
		delay := time.Duration(seconds * float64(time.Second))
		return &delay
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return nil
	}
	current := time.Now()
	if len(now) > 0 {
		current = now[0]
	}
	delay := when.Sub(current)
	if delay < 0 {
		delay = 0
	}
	return &delay
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
