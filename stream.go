package fireworks

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type Stream struct {
	response       *http.Response
	scanner        *bufio.Scanner
	current        Response
	raw            []byte
	json           any
	err            error
	event          string
	data           []string
	hasLastEventID bool
	hasRetry       bool
}

type TypedStream[T any] struct {
	stream  *Stream
	current T
}

func newStream(ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (*Stream, error) {
	ctx, cancel := contextWithRequestTimeout(ctx, opts)
	body, err := mergeExtraBody(body, map[string]any{"stream": true})
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	req, err := client.NewRequest(ctx, http.MethodPost, path, body, opts...)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}
	resp, err := client.httpClient.Do(req)
	if err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, requestError(req, err)
	}
	if cancel != nil {
		if resp.Body != nil {
			resp.Body = &cancelReadCloser{ReadCloser: resp.Body, cancel: cancel}
		} else {
			cancel()
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		return nil, statusError(resp, payload)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Stream{response: resp, scanner: scanner}, nil
}

func (s *Stream) Next() bool {
	for s.scanner.Scan() {
		line := strings.TrimSuffix(s.scanner.Text(), "\r")
		if line == "" {
			yield, stop := s.dispatchEvent()
			if yield {
				return true
			}
			if stop {
				_ = s.Close()
				return false
			}
			continue
		}
		s.decodeLine(line)
	}
	if err := s.scanner.Err(); err != nil {
		s.err = err
	}
	_ = s.Close()
	return false
}

func (s *Stream) decodeLine(line string) {
	if strings.HasPrefix(line, ":") {
		return
	}

	field, value, ok := strings.Cut(line, ":")
	if !ok {
		value = ""
	}
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}

	switch field {
	case "event":
		s.event = value
	case "data":
		s.data = append(s.data, value)
	case "id":
		if !strings.Contains(value, "\x00") {
			s.hasLastEventID = true
		}
	case "retry":
		s.hasRetry = true
	}
}

func (s *Stream) dispatchEvent() (yield bool, stop bool) {
	if s.event == "" && len(s.data) == 0 && !s.hasLastEventID && !s.hasRetry {
		return false, false
	}

	event := s.event
	data := strings.Join(s.data, "\n")
	s.event = ""
	s.data = nil
	s.hasRetry = false

	if strings.HasPrefix(data, "[DONE]") || event == "message_stop" {
		return false, true
	}
	if event == "ping" {
		return false, false
	}

	raw := []byte(data)
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		s.err = err
		return false, true
	}
	s.raw = append(s.raw[:0], raw...)
	s.json = decoded
	if object, ok := decoded.(map[string]any); ok {
		s.current = Response(object)
	} else {
		s.current = nil
	}
	return true, false
}

func (s *Stream) Current() Response {
	return s.current
}

func (s *Stream) CurrentJSON() any {
	return s.json
}

func (s *Stream) RawCurrent() []byte {
	return append([]byte(nil), s.raw...)
}

func (s *Stream) Err() error {
	return s.err
}

func (s *Stream) Response() *http.Response {
	return s.response
}

func (s *Stream) Close() error {
	if s.response == nil || s.response.Body == nil {
		return nil
	}
	return s.response.Body.Close()
}

func newTypedStream[T any](ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (*TypedStream[T], error) {
	stream, err := newStream(ctx, client, path, body, opts...)
	if err != nil {
		return nil, err
	}
	return &TypedStream[T]{stream: stream}, nil
}

func (s *TypedStream[T]) Next() bool {
	if !s.stream.Next() {
		return false
	}
	var out T
	if err := json.Unmarshal(s.stream.RawCurrent(), &out); err != nil {
		s.stream.err = err
		return false
	}
	s.current = out
	return true
}

func (s *TypedStream[T]) Current() T {
	return s.current
}

func (s *TypedStream[T]) Err() error {
	return s.stream.Err()
}

func (s *TypedStream[T]) Response() *http.Response {
	return s.stream.Response()
}

func (s *TypedStream[T]) Close() error {
	return s.stream.Close()
}
