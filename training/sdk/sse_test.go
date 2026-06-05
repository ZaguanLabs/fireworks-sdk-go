package sdk

import (
	"errors"
	"strings"
	"testing"
)

func TestSSEDecoderDataEventAndComments(t *testing.T) {
	decoder := NewSSEDecoder()
	events, err := decoder.Decode(strings.NewReader(": comment\n" +
		"event: completion\n" +
		"data: {\"text\":\"hello\"}\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].Event != "completion" {
		t.Fatalf("event = %q", events[0].Event)
	}
	if events[0].Data != `{"text":"hello"}` {
		t.Fatalf("data = %q", events[0].Data)
	}
}

func TestSSEDecoderMultilineData(t *testing.T) {
	decoder := NewSSEDecoder()
	events, err := decoder.Decode(strings.NewReader("data: first\n" +
		"data: second\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Data != "first\nsecond" {
		t.Fatalf("events = %#v", events)
	}
}

func TestSSEDecoderDoneSentinel(t *testing.T) {
	decoder := NewSSEDecoder()
	events, err := decoder.Decode(strings.NewReader("data: [DONE]\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Data != "[DONE]" {
		t.Fatalf("events = %#v", events)
	}
}

func TestSSEDecoderCRLF(t *testing.T) {
	decoder := NewSSEDecoder()
	events, err := decoder.Decode(strings.NewReader("data: one\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Data != "one" {
		t.Fatalf("events = %#v", events)
	}
}

func TestSSEDecoderDoesNotDispatchUnterminatedEOF(t *testing.T) {
	decoder := NewSSEDecoder()
	events, err := decoder.Decode(strings.NewReader("data: partial"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %#v, want none", events)
	}
}

func TestSSEDecoderDecodeLine(t *testing.T) {
	decoder := NewSSEDecoder()
	if _, ok := decoder.DecodeLine("data: one"); ok {
		t.Fatal("unexpected event before blank line")
	}
	event, ok := decoder.DecodeLine("")
	if !ok {
		t.Fatal("expected event")
	}
	if event.Data != "one" {
		t.Fatalf("data = %q", event.Data)
	}
}

func TestSSETruncationError(t *testing.T) {
	err := &SSETruncationError{Message: "truncated"}
	var trunc *SSETruncationError
	if !errors.As(err, &trunc) {
		t.Fatal("expected errors.As to match SSETruncationError")
	}
	if err.Error() != "truncated" {
		t.Fatalf("error = %q", err.Error())
	}
}
