package tito

import (
	"errors"
	"fmt"
	"math"
)

type turnLocation struct{ segment, turn int }

func (t *trajectory) at(loc turnLocation) *Turn {
	return &t.segments[loc.segment].Turns[loc.turn]
}

func acceptedMessages(turn *Turn) []map[string]any {
	messages := make([]map[string]any, 0, len(turn.Request.Messages)+1)
	return append(append(messages, turn.Request.Messages...), turn.Assistant.Message)
}

// Match the deepest unique accepted conversation checkpoint before comparing
// tokens. Repeated identical assistant histories are intentionally ambiguous.
func (t *trajectory) parent(messages []map[string]any, contract string) (*turnLocation, string) {
	for depth := len(messages); depth > 0; depth-- {
		if messages[depth-1]["role"] != "assistant" {
			continue
		}
		var match *turnLocation
		for _, loc := range t.checkpoints[depth] {
			turn := t.at(loc)
			if !historyExtends(acceptedMessages(turn), messages) {
				continue
			}
			if match != nil {
				return nil, "ambiguous_parent"
			}
			candidate := loc
			match = &candidate
		}
		if match != nil {
			if t.at(*match).FinishReason == "length" {
				return match, "length_truncation"
			}
			if t.segments[match.segment].RenderContractID != contract {
				return match, "contract_change"
			}
			return match, ""
		}
	}
	if len(t.segments) == 0 {
		return nil, "initial"
	}
	if t.segments[t.latestSegment].RenderContractID != contract {
		return nil, "contract_change"
	}
	return nil, "history_rewrite"
}

// ContinuedTurnIDs reports exactly which sampled turns a later accepted
// request included. It validates parent IDs, message history and segment order.
func (a TrajectoryArtifact) ContinuedTurnIDs() (map[string]bool, error) {
	all := make(map[string]*Turn)
	for _, segment := range a.Segments {
		for i := range segment.Turns {
			turn := &segment.Turns[i]
			if _, dup := all[turn.TurnID]; dup {
				return nil, errors.New("duplicate TITO turn ID")
			}
			all[turn.TurnID] = turn
		}
	}
	result := make(map[string]bool)
	for _, segment := range a.Segments {
		for i, turn := range segment.Turns {
			if i > 0 && turn.ParentTurnID != segment.Turns[i-1].TurnID {
				return nil, errors.New("turn does not continue the preceding segment checkpoint")
			}
			if turn.ParentTurnID == "" {
				continue
			}
			parent := all[turn.ParentTurnID]
			if parent == nil || !historyExtends(acceptedMessages(parent), turn.Request.Messages) {
				return nil, fmt.Errorf("invalid parent history for turn %s", turn.TurnID)
			}
			result[turn.ParentTurnID] = true
		}
	}
	return result, nil
}

type MalformedToolCall struct {
	TurnID     string
	ReasonCode string
}

func retainedReason(failure map[string]any) bool {
	closed, specified := failure["closed_tool_action"].(bool)
	if specified && !closed {
		return false
	}
	switch failure["parser_reason_code"] {
	case "tool_missing_name", "tool_unknown_name", "tool_arguments_invalid_json", "tool_arguments_not_object":
		return true
	case "tool_invalid_argument":
		return closed
	}
	return false
}

func exactToolSample(finish string, probs []*float64, n int) bool {
	if n == 0 || len(probs) != n || (finish != "stop" && finish != "tool_calls") {
		return false
	}
	for _, p := range probs {
		if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) {
			return false
		}
	}
	return true
}

// MalformedToolCall returns the exact rejected terminal sample eligible for scoring.
func (a TrajectoryArtifact) MalformedToolCall() *MalformedToolCall {
	failure, _ := a.Metadata["tito_failure"].(map[string]any)
	id, _ := a.Metadata["tito_malformed_tool_call_id"].(string)
	if a.Status != TrajectoryStatusFailed || a.TerminalReason != "tito_sdk_parse_error" ||
		failure["code"] != a.TerminalReason || failure["origin"] != "sdk_parser" ||
		failure["debug_storage_error"] != nil || !retainedReason(failure) || id == "" || id != failure["call_id"] {
		return nil
	}
	for _, call := range a.Calls {
		if call.CallID != id || call.Kind != CallKindPolicy || call.ClassificationSource == "fail_closed" ||
			call.Outcome != CallOutcomeModelMalformed || call.ErrorCode != a.TerminalReason || call.TurnID == "" {
			continue
		}
		for _, segment := range a.Segments {
			if segment.ClosedReason != "malformed_tool_call" || len(segment.Turns) == 0 {
				continue
			}
			turn := segment.Turns[len(segment.Turns)-1]
			if turn.TurnID == call.TurnID && turn.Assistant.OutputKind == "malformed_tool_call" && exactToolSample(turn.FinishReason, turn.SamplingLogprobs, len(turn.ExactCompletionIDs)) {
				return &MalformedToolCall{turn.TurnID, failure["parser_reason_code"].(string)}
			}
		}
	}
	return nil
}
