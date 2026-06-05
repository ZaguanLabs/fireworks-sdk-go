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
	response *http.Response
	scanner  *bufio.Scanner
	current  Response
	err      error
}

type TypedStream[T any] struct {
	stream  *Stream
	current T
}

func newStream(ctx context.Context, client *Client, path string, body any, opts ...RequestOption) (*Stream, error) {
	req, err := client.NewRequest(ctx, http.MethodPost, path, body, opts...)
	if err != nil {
		return nil, err
	}
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return nil, err
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
		line := strings.TrimSpace(s.scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return false
		}
		var out Response
		if err := json.Unmarshal([]byte(data), &out); err != nil {
			s.err = err
			return false
		}
		s.current = out
		return true
	}
	if err := s.scanner.Err(); err != nil {
		s.err = err
	}
	return false
}

func (s *Stream) Current() Response {
	return s.current
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
	payload, err := json.Marshal(s.stream.Current())
	if err != nil {
		s.stream.err = err
		return false
	}
	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
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
