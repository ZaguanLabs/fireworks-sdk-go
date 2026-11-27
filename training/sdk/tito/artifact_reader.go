package tito

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// Read large collections incrementally so the JSON decoder need only buffer
// the largest turn, call, or metadata record alongside the decoded trajectory.
func readArtifactObject(d *json.Decoder, fields map[string]any, special func(string) (bool, error)) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("expected artifact object")
	}
	seen := make(map[string]bool)
	for d.More() {
		k, err := d.Token()
		if err != nil {
			return err
		}
		key, ok := k.(string)
		if !ok || seen[key] {
			return fmt.Errorf("invalid or duplicate artifact field %v", k)
		}
		seen[key] = true
		if dest, ok := fields[key]; ok {
			if err := d.Decode(dest); err != nil {
				return err
			}
			continue
		}
		handled, err := special(key)
		if err != nil {
			return err
		}
		if !handled {
			var extra json.RawMessage
			if err := d.Decode(&extra); err != nil {
				return err
			}
		}
	}
	_, err = d.Token()
	return err
}

func readArtifactArray[T any](d *json.Decoder, dest *[]T, item func(*T) error) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	} // Older Go archives wrote null for empty slices.
	if tok != json.Delim('[') {
		return fmt.Errorf("expected artifact array")
	}
	*dest = []T{}
	for d.More() {
		var v T
		if err := item(&v); err != nil {
			return err
		}
		*dest = append(*dest, v)
	}
	_, err = d.Token()
	return err
}
func readArtifact(d *json.Decoder) (a TrajectoryArtifact, err error) {
	fields := map[string]any{
		"schema_version": &a.SchemaVersion, "lineage_mode": &a.LineageMode, "trajectory_id": &a.TrajectoryID,
		"serving_affinity_key_hash": &a.ServingAffinityKeyHash, "metadata": &a.Metadata, "status": &a.Status,
		"terminal_reason": &a.TerminalReason, "metrics": &a.Metrics, "started_at": &a.StartedAt, "finished_at": &a.FinishedAt,
	}
	err = readArtifactObject(d, fields, func(key string) (bool, error) {
		switch key {
		case "prompt_prefixes":
			return true, fmt.Errorf("unsupported prompt prefix table")
		case "calls":
			return true, readArtifactArray(d, &a.Calls, func(call *CallRecord) error { return d.Decode(call) })
		case "response_attempts":
			return true, readArtifactArray(d, &a.ResponseAttempts, func(attempt *ResponseAttempt) error { return d.Decode(attempt) })
		case "segments":
			return true, readArtifactArray(d, &a.Segments, func(s *SegmentResult) error {
				return readArtifactObject(d, map[string]any{"segment_id": &s.SegmentID, "start_reason": &s.StartReason, "render_contract_id": &s.RenderContractID, "closed_reason": &s.ClosedReason}, func(key string) (bool, error) {
					if key != "turns" {
						return false, nil
					}
					return true, readArtifactArray(d, &s.Turns, func(t *Turn) error {
						var raw json.RawMessage
						if err := d.Decode(&raw); err != nil {
							return err
						}
						return t.decodeJSON(raw, true)
					})
				})
			})
		}
		return false, nil
	})
	return a, err
}

// encoding/json replaces malformed UTF-8 with U+FFFD. Validate the original
// bytes as they are decompressed, including characters split across reads.
type utf8Validator struct {
	tail      [utf8.UTFMax]byte
	remaining int
}

func (v *utf8Validator) Write(data []byte) (int, error) {
	total := len(data)
	invalid := func() (int, error) { return 0, fmt.Errorf("invalid UTF-8 in TITO artifact") }
	if v.remaining > 0 {
		for len(data) > 0 && !utf8.FullRune(v.tail[:v.remaining]) {
			v.tail[v.remaining] = data[0]
			v.remaining++
			data = data[1:]
		}
		if !utf8.FullRune(v.tail[:v.remaining]) {
			return total, nil
		}
		if !utf8.Valid(v.tail[:v.remaining]) {
			return invalid()
		}
		v.remaining = 0
	}
	for i := len(data) - 1; i >= 0 && len(data)-i < utf8.UTFMax; i-- {
		if utf8.RuneStart(data[i]) {
			if !utf8.FullRune(data[i:]) {
				v.remaining = copy(v.tail[:], data[i:])
				data = data[:i]
			}
			break
		}
	}
	if !utf8.Valid(data) {
		return invalid()
	}
	return total, nil
}
