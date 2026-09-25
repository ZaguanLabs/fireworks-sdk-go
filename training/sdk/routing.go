package sdk

import (
	"encoding/json"
	"fmt"
)

type RoutingMatrixFormat string

const (
	RoutingBase64Inline RoutingMatrixFormat = "base64_inline"
	RoutingParquetV1    RoutingMatrixFormat = "parquet_v1"
	R3StoreHeader                           = "x-fireworks-r3-store-id"
	R3TTLHeader                             = "x-fireworks-r3-ttl-seconds"
)

// RoutingFile preserves server-provided metadata, including future file attributes.
type RoutingFile map[string]any
type RoutingSpan struct {
	InputTokenStart int  `json:"input_token_start"`
	Count           int  `json:"count"`
	FileIndex       *int `json:"file_index,omitempty"`
	FileRowStart    *int `json:"file_row_start,omitempty"`
}

// RoutingReferences retains compact file ranges; helpers never read routing files.
type RoutingReferences struct {
	Length int           `json:"length"`
	Files  []RoutingFile `json:"files"`
	Spans  []RoutingSpan `json:"spans"`
}

func (r RoutingReferences) Validate() error {
	if r.Length < 0 {
		return fmt.Errorf("invalid R3 reference length")
	}
	pos := 0
	for _, s := range r.Spans {
		if s.Count <= 0 || s.InputTokenStart != pos || s.Count > r.Length-pos {
			return fmt.Errorf("R3 ranges must cover input positions exactly once")
		}
		if s.FileIndex != nil {
			i := *s.FileIndex
			if i < 0 || i >= len(r.Files) {
				return fmt.Errorf("invalid R3 file index")
			}
			rows, ok := intFromStrictAny(r.Files[i]["row_count"])
			if !ok || s.FileRowStart == nil || *s.FileRowStart < 0 || *s.FileRowStart > rows || s.Count > rows-*s.FileRowStart {
				return fmt.Errorf("invalid R3 file range")
			}
			if r.Files[i]["format"] != "parquet_v1" {
				return fmt.Errorf("unsupported R3 file format")
			}
		}
		pos += s.Count
	}
	if pos != r.Length {
		return fmt.Errorf("R3 ranges do not match input length")
	}
	return nil
}
func ParseRoutingReferences(value any) (*RoutingReferences, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	for _, key := range []string{"length", "files", "spans"} {
		if raw, ok := fields[key]; !ok || string(raw) == "null" {
			return nil, fmt.Errorf("R3 references require %s", key)
		}
	}
	var r RoutingReferences
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r.Compact(), nil
}
func (r RoutingReferences) Compact() *RoutingReferences {
	out := &RoutingReferences{Length: r.Length, Files: []RoutingFile{}, Spans: []RoutingSpan{}}
	indices := map[string]int{}
	for _, span := range r.Spans {
		if span.FileIndex != nil {
			file := r.Files[*span.FileIndex]
			data, _ := json.Marshal(file)
			key := string(data)
			i, ok := indices[key]
			if !ok {
				i = len(out.Files)
				out.Files = append(out.Files, RoutingFile(cloneAnyMap(file)))
				indices[key] = i
			}
			span.FileIndex = intPointer(i)
			if span.FileRowStart != nil {
				span.FileRowStart = intPointer(*span.FileRowStart)
			}
		}
		if len(out.Spans) > 0 {
			prev := &out.Spans[len(out.Spans)-1]
			same := (prev.FileIndex == nil && span.FileIndex == nil) || (prev.FileIndex != nil && span.FileIndex != nil && *prev.FileIndex == *span.FileIndex && *prev.FileRowStart+prev.Count == *span.FileRowStart)
			if same && prev.InputTokenStart+prev.Count == span.InputTokenStart {
				prev.Count += span.Count
				continue
			}
		}
		out.Spans = append(out.Spans, span)
	}
	return out
}
func (r RoutingReferences) Slice(start, end int) (*RoutingReferences, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	if start < 0 {
		start += r.Length
	}
	if end < 0 {
		end += r.Length
	}
	start = maxInt(0, minInt(start, r.Length))
	end = maxInt(start, minInt(end, r.Length))
	out := RoutingReferences{Length: end - start, Files: r.Files}
	for _, s := range r.Spans {
		left := maxInt(start, s.InputTokenStart)
		right := minInt(end, s.InputTokenStart+s.Count)
		if left >= right {
			continue
		}
		if s.FileIndex != nil {
			s.FileRowStart = intPointer(*s.FileRowStart + left - s.InputTokenStart)
		}
		s.InputTokenStart = left - start
		s.Count = right - left
		out.Spans = append(out.Spans, s)
	}
	return out.Compact(), nil
}
func (r RoutingReferences) HasGaps() bool {
	for _, s := range r.Spans {
		if s.FileIndex == nil {
			return true
		}
	}
	return false
}
func (r RoutingReferences) ModelInputKwargs() map[string]any {
	return map[string]any{"routing_matrix_format": "parquet_v1", "routing_references": r, "routing_matrices": nil}
}

// ConcatRouting accepts references or inline []string. Nonempty inline rows cannot
// share a trajectory with Parquet references; empty inline rows represent gaps.
func ConcatRouting(values ...any) (any, error) {
	parquet := false
	for _, v := range values {
		switch v.(type) {
		case RoutingReferences, *RoutingReferences:
			parquet = true
		}
	}
	if !parquet {
		var out []string
		for _, v := range values {
			rows, ok := v.([]string)
			if !ok {
				return nil, fmt.Errorf("invalid inline routing")
			}
			out = append(out, rows...)
		}
		return out, nil
	}
	out := RoutingReferences{Files: []RoutingFile{}, Spans: []RoutingSpan{}}
	for _, v := range values {
		var r *RoutingReferences
		switch value := v.(type) {
		case RoutingReferences:
			r = &value
		case *RoutingReferences:
			r = value
		case []string:
			for _, row := range value {
				if row != "" {
					return nil, fmt.Errorf("cannot mix inline and Parquet routes")
				}
			}
			if len(value) > 0 {
				out.Spans = append(out.Spans, RoutingSpan{InputTokenStart: out.Length, Count: len(value)})
				out.Length += len(value)
			}
			continue
		default:
			return nil, fmt.Errorf("invalid routing value")
		}
		if r == nil {
			return nil, fmt.Errorf("nil routing reference")
		}
		if err := r.Validate(); err != nil {
			return nil, err
		}
		offset := len(out.Files)
		out.Files = append(out.Files, r.Files...)
		for _, span := range r.Spans {
			span.InputTokenStart += out.Length
			if span.FileIndex != nil {
				span.FileIndex = intPointer(*span.FileIndex + offset)
			}
			out.Spans = append(out.Spans, span)
		}
		out.Length += r.Length
	}
	return out.Compact(), nil
}
func (r RoutingReferences) Mask(mask []bool) (*RoutingReferences, error) {
	if len(mask) != r.Length {
		return nil, fmt.Errorf("routing mask length mismatch")
	}
	empty, err := r.Slice(0, 0)
	if err != nil {
		return nil, err
	}
	pieces := []any{empty}
	for start := 0; start < len(mask); {
		end := start + 1
		for end < len(mask) && mask[end] == mask[start] {
			end++
		}
		if mask[start] {
			part, err := r.Slice(start, end)
			if err != nil {
				return nil, err
			}
			pieces = append(pieces, part)
		} else {
			pieces = append(pieces, make([]string, end-start))
		}
		start = end
	}
	out, err := ConcatRouting(pieces...)
	if err != nil {
		return nil, err
	}
	return out.(*RoutingReferences), nil
}
