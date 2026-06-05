package sdk

import (
	"bufio"
	"io"
	"strings"
)

type SSETruncationError struct {
	Message string
}

func (e *SSETruncationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "server closed the SSE completion stream mid-generation"
}

type SSEEvent struct {
	Data  string
	Event string
}

type SSEDecoder struct {
	event string
	data  []string
}

func NewSSEDecoder() *SSEDecoder {
	return &SSEDecoder{}
}

func (d *SSEDecoder) Decode(reader io.Reader) ([]SSEEvent, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var events []SSEEvent
	for scanner.Scan() {
		event, ok := d.DecodeLine(scanner.Text())
		if ok {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func (d *SSEDecoder) DecodeLine(line string) (SSEEvent, bool) {
	line = strings.TrimSuffix(line, "\r")
	if line == "" {
		if len(d.data) == 0 && d.event == "" {
			return SSEEvent{}, false
		}
		event := SSEEvent{Data: strings.Join(d.data, "\n"), Event: d.event}
		d.data = nil
		d.event = ""
		return event, true
	}
	if strings.HasPrefix(line, ":") {
		return SSEEvent{}, false
	}

	field, value, ok := strings.Cut(line, ":")
	if !ok {
		value = ""
	}
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}

	switch field {
	case "data":
		d.data = append(d.data, value)
	case "event":
		d.event = value
	}
	return SSEEvent{}, false
}
