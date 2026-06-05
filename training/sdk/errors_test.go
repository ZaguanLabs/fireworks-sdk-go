package sdk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFormatSDKErrorBasicOutput(t *testing.T) {
	result := FormatSDKError("Job failed", "bad model", "Fix the model name")
	if !strings.Contains(result, "ERROR: Job failed") {
		t.Fatalf("result = %q", result)
	}
	if !strings.Contains(result, "Cause: bad model") {
		t.Fatalf("result = %q", result)
	}
	if !strings.Contains(result, "Solution: Fix the model name") {
		t.Fatalf("result = %q", result)
	}
	if !strings.Contains(result, "Agent debug: "+AgentDebugInstructions) {
		t.Fatalf("result = %q", result)
	}
}

func TestFormatSDKErrorWithDocsURL(t *testing.T) {
	result := FormatSDKError("Job failed", "bad model", "Fix it", SDKErrorFormatOptions{DocsURL: "https://docs.example.com"})
	if !strings.Contains(result, "Docs: https://docs.example.com") {
		t.Fatalf("result = %q", result)
	}
}

func TestFormatSDKErrorWithoutDocsURL(t *testing.T) {
	result := FormatSDKError("Job failed", "bad model", "Fix it")
	if strings.Contains(result, "Docs:") {
		t.Fatalf("result = %q", result)
	}
}

func TestFormatSDKErrorLineOrder(t *testing.T) {
	result := FormatSDKError("W", "C", "S", SDKErrorFormatOptions{DocsURL: "D"})
	lines := strings.Split(result, "\n")
	prefixes := []string{"ERROR:", "  Cause:", "  Solution:", "  Agent debug:", "  Docs:"}
	for i, prefix := range prefixes {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Fatalf("line %d = %q, want prefix %q", i, lines[i], prefix)
		}
	}
}

func TestFormatSDKErrorShowSupport(t *testing.T) {
	result := FormatSDKError("Oops", "reason", "Try again", SDKErrorFormatOptions{ShowSupport: true})
	if !strings.Contains(result, "Support: "+DiscordURL) {
		t.Fatalf("result = %q", result)
	}

	result = FormatSDKError("Oops", "reason", "Try again")
	if strings.Contains(result, "Support:") {
		t.Fatalf("result = %q", result)
	}
}

func TestHTTPStatusHintsMentionTrainingScopedKeys(t *testing.T) {
	hint := HTTPStatusHints[http.StatusUnauthorized]
	if !strings.Contains(hint, "training-scoped") || !strings.Contains(hint, "inference-only") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestHTTPStatusHintsMentionValidKeyWrongResourceScope(t *testing.T) {
	hint := HTTPStatusHints[http.StatusForbidden]
	if !strings.Contains(hint, "key is valid") || !strings.Contains(hint, "resource") {
		t.Fatalf("hint = %q", hint)
	}
}

func TestParseAPIErrorString(t *testing.T) {
	if got := ParseAPIErrorBody([]byte(`{"error":"something went wrong"}`)); got != "something went wrong" {
		t.Fatalf("error = %q", got)
	}
}

func TestParseAPIErrorDictWithMessage(t *testing.T) {
	if got := ParseAPIErrorBody([]byte(`{"error":{"message":"bad request","code":400}}`)); got != "bad request" {
		t.Fatalf("error = %q", got)
	}
}

func TestParseAPIErrorDictWithoutMessage(t *testing.T) {
	got := ParseAPIErrorBody([]byte(`{"error":{"code":500}}`))
	if !strings.Contains(got, "code") {
		t.Fatalf("error = %q", got)
	}
}

func TestParseAPIErrorPlainTextBody(t *testing.T) {
	if got := ParseAPIErrorBody([]byte("  plain text error  ")); got != "plain text error" {
		t.Fatalf("error = %q", got)
	}
}

func TestParseAPIErrorLongTextTruncatedTo200(t *testing.T) {
	got := ParseAPIErrorBody([]byte(strings.Repeat("x", 500)))
	if len(got) != 200 {
		t.Fatalf("len(error) = %d", len(got))
	}
}

func TestParseAPIErrorNoErrorKeyReturnsWholeBody(t *testing.T) {
	got := ParseAPIErrorBody([]byte(`{"detail":"not found"}`))
	if !strings.Contains(got, "detail") {
		t.Fatalf("error = %q", got)
	}
}

func TestParseAPIErrorRestoresResponseBody(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(bytes.NewBufferString(`{"error":"bad"}`))}
	if got := ParseAPIError(resp); got != "bad" {
		t.Fatalf("error = %q", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"error":"bad"}` {
		t.Fatalf("restored body = %q", string(body))
	}
}

func TestIsRetryableStatusCode(t *testing.T) {
	for _, code := range []int{408, 429, 500, 502, 503, 504} {
		if !IsRetryableStatusCode(code) {
			t.Fatalf("status %d should be retryable", code)
		}
	}
	for _, code := range []int{200, 201, 400, 401, 403, 404, 409, 425} {
		if IsRetryableStatusCode(code) {
			t.Fatalf("status %d should not be retryable", code)
		}
	}
}

func TestRequestWithRetriesSuccessfulRequest(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK}
	calls := 0
	got, err := RequestWithRetries(func() (*http.Response, error) {
		calls++
		return resp, nil
	}, noSleepRetryOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got != resp || calls != 1 {
		t.Fatalf("resp = %v calls = %d", got, calls)
	}
}

func TestRequestWithRetriesRetriesConnectionError(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK}
	calls := 0
	got, err := RequestWithRetries(func() (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, context.DeadlineExceeded
		}
		return resp, nil
	}, noSleepRetryOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got != resp || calls != 2 {
		t.Fatalf("resp = %v calls = %d", got, calls)
	}
}

func TestRequestWithRetriesRetries503(t *testing.T) {
	calls := 0
	got, err := RequestWithRetries(func() (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: http.StatusServiceUnavailable}, nil
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	}, noSleepRetryOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got.StatusCode != http.StatusOK || calls != 2 {
		t.Fatalf("status = %d calls = %d", got.StatusCode, calls)
	}
}

func TestRequestWithRetriesExhaustsMaxWaitTime(t *testing.T) {
	now := time.Unix(0, 0)
	resp := &http.Response{StatusCode: http.StatusServiceUnavailable}
	got, err := RequestWithRetries(
		func() (*http.Response, error) { return resp, nil },
		RequestRetryOptions{
			MaxWaitTime: 5 * time.Second,
			Now: func() time.Time {
				current := now
				now = now.Add(100 * time.Second)
				return current
			},
			Sleep: func(time.Duration) {},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != resp {
		t.Fatalf("resp = %v, want %v", got, resp)
	}
}

func TestRequestWithRetriesNonRetryableStatusReturnedImmediately(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadRequest}
	calls := 0
	got, err := RequestWithRetries(func() (*http.Response, error) {
		calls++
		return resp, nil
	}, noSleepRetryOptions())
	if err != nil {
		t.Fatal(err)
	}
	if got != resp || calls != 1 {
		t.Fatalf("resp = %v calls = %d", got, calls)
	}
}

func TestRequestWithRetriesConnectionErrorExhausts(t *testing.T) {
	now := time.Unix(0, 0)
	_, err := RequestWithRetries(
		func() (*http.Response, error) { return nil, context.DeadlineExceeded },
		RequestRetryOptions{
			MaxWaitTime: 5 * time.Second,
			Now: func() time.Time {
				current := now
				now = now.Add(100 * time.Second)
				return current
			},
			Sleep: func(time.Duration) {},
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func noSleepRetryOptions() RequestRetryOptions {
	return RequestRetryOptions{Sleep: func(time.Duration) {}}
}
