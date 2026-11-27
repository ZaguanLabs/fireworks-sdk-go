package sdk

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type kimiArgument struct{ key, kind, text string }

// Build an equivalent strict JSON stream for the K3.1 reader, which accepts
// literal control characters in quoted strings. Keep offsets into the original
// input so non-string values retain the exact source spelling.
func kimiStrictInput(s string) (string, []int) {
	if strings.IndexFunc(s, func(r rune) bool { return r < 32 }) < 0 {
		return s, nil
	}
	var b strings.Builder
	offsets := []int{0}
	quoted, escaped := false, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quoted && ch < 32 {
			fragment := fmt.Sprintf("\\u%04x", ch)
			b.WriteString(fragment)
			for j := 1; j < len(fragment); j++ {
				offsets = append(offsets, i)
			}
		} else {
			b.WriteByte(ch)
		}
		offsets = append(offsets, i+1)
		if escaped {
			escaped = false
		} else if ch == '\\' && quoted {
			escaped = true
		} else if ch == '"' {
			quoted = !quoted
		}
	}
	return b.String(), offsets
}
func kimiJSONKind(raw string) string {
	switch raw[0] {
	case '"':
		return "string"
	case '{':
		return "object"
	case '[':
		return "array"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

func kimiPythonNumber(n json.Number) (string, error) {
	value := string(n)
	if !strings.ContainsAny(value, ".eE") {
		if value == "-0" {
			return "0", nil
		}
		return value, nil
	}
	f, err := n.Float64()
	if err != nil {
		return "", err
	}
	// Python uses fixed point for decimal exponents in [-4, 16).
	sci := strconv.FormatFloat(f, 'e', -1, 64)
	e, _ := strconv.Atoi(sci[strings.IndexByte(sci, 'e')+1:])
	if e < -4 || e >= 16 {
		return sci, nil
	}
	result := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(result, ".") {
		result += ".0"
	}
	return result, nil
}

// Render Python's default JSON separators while retaining object member order.
func kimiPythonValue(dec *json.Decoder) (string, error) {
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	switch v := tok.(type) {
	case json.Delim:
		var parts []string
		index := map[string]int{}
		for dec.More() {
			var key string
			if v == '{' {
				k, err := dec.Token()
				if err != nil {
					return "", err
				}
				key = k.(string)
			}
			item, err := kimiPythonValue(dec)
			if err != nil {
				return "", err
			}
			if v == '{' {
				encoded, err := kimiCompact(key)
				if err != nil {
					return "", err
				}
				item = encoded + ": " + item
				if i, exists := index[key]; exists {
					parts[i] = item
					continue
				}
				index[key] = len(parts)
			}
			parts = append(parts, item)
		}
		if _, err = dec.Token(); err != nil {
			return "", err
		}
		if v == '{' {
			return "{" + strings.Join(parts, ", ") + "}", nil
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	case json.Number:
		return kimiPythonNumber(v)
	default:
		return kimiCompact(v)
	}
}

func kimiParseObject(raw, profile string, fromMap bool) ([]kimiArgument, error) {
	strict := raw
	var offsets []int
	if profile == "kimi_k31" {
		strict, offsets = kimiStrictInput(raw)
	}
	dec := json.NewDecoder(strings.NewReader(strict))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	args := []kimiArgument{}
	indices := map[string]int{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("expected argument key")
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		literal := string(value)
		kind := kimiJSONKind(literal)
		var text string
		switch {
		case kind == "string":
			if err := json.Unmarshal(value, &text); err != nil {
				return nil, err
			}
		case profile == "kimi_k31" && !fromMap:
			end := int(dec.InputOffset())
			start := end - len(value)
			if offsets != nil {
				start, end = offsets[start], offsets[end]
			}
			text = raw[start:end]
		default:
			nested := json.NewDecoder(strings.NewReader(literal))
			nested.UseNumber()
			text, err = kimiPythonValue(nested)
			if err != nil {
				return nil, err
			}
		}
		arg := kimiArgument{key, kind, text}
		if i, ok := indices[key]; ok && profile == "kimi_k3" {
			args[i] = arg
			continue
		}
		indices[key] = len(args)
		args = append(args, arg)
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if profile == "kimi_k3" {
		if _, err := dec.Token(); err != io.EOF {
			return nil, fmt.Errorf("trailing JSON data")
		}
	}
	return args, nil
}

func kimiArguments(profile string, input any) ([]kimiArgument, string, error) {
	if input == nil {
		return nil, "", nil
	}
	var raw string
	var fromMap bool
	switch v := input.(type) {
	case string:
		raw = v
	case json.RawMessage:
		raw = string(v)
	default:
		var err error
		raw, err = kimiCompact(v)
		if err != nil {
			return nil, "", err
		}
		if !strings.HasPrefix(raw, "{") {
			return nil, "", fmt.Errorf("tool arguments must be an object or a JSON object string")
		}
		fromMap = true
	}
	if raw == "" || profile == "kimi_k3" && strings.TrimSpace(raw) == "" {
		return nil, "", nil
	}
	args, err := kimiParseObject(raw, profile, fromMap)
	if err != nil {
		if profile == "kimi_k3" && json.Valid([]byte(raw)) && strings.TrimSpace(raw)[0] != '{' {
			return nil, "", fmt.Errorf("K3 tool arguments must be a JSON object")
		}
		return nil, raw, nil
	}
	return args, "", nil
}
