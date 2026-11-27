// Kimi K3 and K3.1 XTML rendering, adapted from the release encoders by
// Moonshot AI. See licenses/Kimi-K3.txt and licenses/Kimi-K3.1-Pro.txt.
package sdk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// KimiChatSegment distinguishes structural tokens from text supplied by callers.
// The tokenizer must preserve segment boundaries when encoding ordinary text.
type KimiChatSegment struct {
	Text         string
	AllowSpecial bool
}

type KimiChatOptions struct {
	Thinking, AddGenerationPrompt *bool
	ThinkingEffort                *string
	PreserveThinking              bool
	ImagePrompts                  []string
	Tools                         []map[string]any
	// ToolsJSON preserves caller-supplied tool member order for K2 Base. Supply either Tools or ToolsJSON.
	ToolsJSON                      json.RawMessage
	ToolChoice                     string
	ResponseFormat, ResponseSchema any
}

type kimiRenderer struct {
	profile    string
	options    KimiChatOptions
	segments   []KimiChatSegment
	imageIndex int
	calls      []map[string]any
	toolIndex  int
}

func (r *kimiRenderer) emit(s string, special bool) {
	if s != "" {
		r.segments = append(r.segments, KimiChatSegment{s, special})
	}
}

func (r *kimiRenderer) open(name string, attrs ...string) {
	r.emit("<|open|>", true)
	r.emit(name, false)
	for i := 0; i < len(attrs); i += 2 {
		r.emit(" "+attrs[i], false)
		r.emit("=\"", false)
		r.emit(strings.NewReplacer("&", "&amp;", "\"", "&quot;").Replace(attrs[i+1]), false)
		r.emit("\"", false)
	}
	r.emit("<|sep|>", true)
}
func (r *kimiRenderer) close(name string) {
	r.emit("<|close|>", true)
	r.emit(name, false)
	r.emit("<|sep|>", true)
}
func (r *kimiRenderer) finishMessage() { r.close("message"); r.emit("<|end_of_msg|>", true) }
func (r *kimiRenderer) system(kind, text string) {
	r.open("message", "role", "system", "type", kind)
	r.emit(text, false)
	r.finishMessage()
}
func (r *kimiRenderer) image() error {
	const placeholder = "<|kimi_image_placeholder|>"
	prompts := r.options.ImagePrompts
	if prompts == nil {
		r.emit(placeholder, true)
		return nil
	}
	if r.imageIndex >= len(prompts) {
		return fmt.Errorf("more image placeholders than prompts")
	}
	r.emit(prompts[r.imageIndex], true)
	r.imageIndex++
	return nil
}
func (r *kimiRenderer) text(s string) error {
	if r.options.ImagePrompts == nil {
		r.emit(s, false)
		return nil
	}
	for i, part := range strings.Split(s, "<|kimi_image_placeholder|>") {
		if i > 0 {
			if err := r.image(); err != nil {
				return err
			}
		}
		r.emit(part, false)
	}
	return nil
}

func kimiObjects(v any) ([]map[string]any, error) {
	switch xs := v.(type) {
	case nil:
		return nil, nil
	case []map[string]any:
		return xs, nil
	case []any:
		out := make([]map[string]any, len(xs))
		for i, x := range xs {
			var ok bool
			out[i], ok = x.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("item %d must be an object", i)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a list of objects, got %T", v)
	}
}
func (r *kimiRenderer) content(v any) error {
	if s, ok := v.(string); ok {
		return r.text(s)
	}
	parts, err := kimiObjects(v)
	if err != nil {
		return err
	}
	for _, p := range parts {
		switch p["type"] {
		case "image", "image_url":
			err = r.image()
		default:
			s, ok := p["text"].(string)
			if !ok {
				return fmt.Errorf("content part requires text")
			}
			err = r.text(s)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func kimiFunction(call map[string]any) map[string]any {
	if f, ok := call["function"].(map[string]any); ok {
		return f
	}
	return call
}
func kimiMapString(m map[string]any, key string) string { s, _ := m[key].(string); return s }

type kimiMatchedTool struct {
	position int
	name     string
}

func kimiOrderTools(tools, calls []map[string]any) []map[string]any {
	matches := make(map[string]kimiMatchedTool, len(calls))
	for i, c := range calls {
		if id := c["id"]; id != nil {
			key := fmt.Sprint(id)
			if _, exists := matches[key]; !exists {
				matches[key] = kimiMatchedTool{i, kimiMapString(kimiFunction(c), "name")}
			}
		}
	}
	type match struct {
		message map[string]any
		ref     kimiMatchedTool
	}
	sorted := make([]match, len(tools))
	for i, m := range tools {
		id, ok := m["tool_call_id"]
		if !ok {
			id = m["id"]
		}
		ref, ok := matches[fmt.Sprint(id)]
		if id == nil || !ok {
			return tools
		}
		sorted[i] = match{m, ref}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ref.position < sorted[j].ref.position })
	out := make([]map[string]any, len(sorted))
	for i, item := range sorted {
		out[i] = item.message
		if item.ref.name != "" {
			copy := make(map[string]any, len(item.message)+1)
			for k, v := range item.message {
				copy[k] = v
			}
			copy["tool"] = item.ref.name
			if _, ok := copy["name"]; ok {
				copy["name"] = item.ref.name
			}
			out[i] = copy
		}
	}
	return out
}

func kimiCompact(value any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
func (r *kimiRenderer) declare(tools any, dynamic bool) error {
	spec, err := kimiCompact(tools)
	if err != nil {
		return err
	}
	header := "# Tools\nHere are the available tools, described in JSONSchema."
	if dynamic {
		header = "## New Tools Available\nThe system dynamically extends the toolset via lazy-loading.\nYou have access to all existing and extended tools.\nHere are the specs for the extended tools."
	}
	r.system("tool-declare", header+"\n\n\x60\x60\x60json\n"+spec+"\n\x60\x60\x60")
	return nil
}
func (r *kimiRenderer) assistant(m map[string]any, thinking bool) error {
	var err error
	r.calls, err = kimiObjects(m["tool_calls"])
	if err != nil {
		return err
	}
	r.toolIndex = 0
	if thinking {
		r.open("think")
		reason := kimiMapString(m, "reasoning_content")
		if reason == "" {
			reason = kimiMapString(m, "reasoning")
		}
		if r.profile == "kimi_k31" || strings.TrimSpace(reason) != "" {
			if err := r.text(reason); err != nil {
				return err
			}
		}
		r.close("think")
	}
	r.open("response")
	if err := r.content(m["content"]); err != nil {
		return err
	}
	r.close("response")
	if len(r.calls) == 0 {
		return nil
	}
	r.open("tools")
	for i, c := range r.calls {
		f := kimiFunction(c)
		name, ok := f["name"].(string)
		if !ok {
			return fmt.Errorf("tool call %d requires a function name", i+1)
		}
		r.open("call", "tool", name, "index", strconv.Itoa(i+1))
		args, block, err := kimiArguments(r.profile, f["arguments"])
		if err != nil {
			return err
		}
		if block != "" {
			r.open("json", "type", "object")
			if err := r.text(block); err != nil {
				return err
			}
			r.close("json")
		}
		for _, a := range args {
			r.open("argument", "key", a.key, "type", a.kind)
			if err := r.text(a.text); err != nil {
				return err
			}
			r.close("argument")
		}
		r.close("call")
	}
	r.close("tools")
	return nil
}
func (r *kimiRenderer) message(m map[string]any, thinking bool) error {
	role := kimiMapString(m, "role")
	attrs := []string{"role", role}
	switch role {
	case "system":
		if tools := m["tools"]; tools != nil {
			items, err := kimiObjects(tools)
			if err != nil {
				return err
			}
			if len(items) > 0 {
				return r.declare(items, true)
			}
		}
	case "assistant", "user":
	case "tool":
		r.toolIndex++
		name, ok := m["tool"]
		if !ok {
			name = m["name"]
		}
		if name == nil && r.toolIndex <= len(r.calls) {
			name = kimiFunction(r.calls[r.toolIndex-1])["name"]
		}
		if name == nil {
			return fmt.Errorf("cannot resolve tool name for result %d", r.toolIndex)
		}
		attrs = append(attrs, "tool", fmt.Sprint(name), "index", strconv.Itoa(r.toolIndex))
	default:
		if r.profile == "kimi_k31" || m != nil && m["role"] == nil {
			return fmt.Errorf("unknown message role %q", role)
		}
		return nil
	}
	if role != "tool" {
		if name := kimiMapString(m, "name"); name != "" {
			attrs = append(attrs, "name", name)
		}
	}
	r.open("message", attrs...)
	var err error
	if role == "assistant" {
		err = r.assistant(m, thinking)
	} else {
		err = r.content(m["content"])
	}
	if err != nil {
		return err
	}
	r.finishMessage()
	return nil
}

func (r *kimiRenderer) format() error {
	o := r.options
	switch o.ToolChoice {
	case "required":
		r.system("tool-choice", "The system is invoked with `tool_choice=required`.\nYou MUST call tools in the next message.")
	case "none":
		r.system("tool-choice", "The system is invoked with `tool_choice=none`.\nYou MUST NOT call any tools in the next message.")
	}
	kind, _ := o.ResponseFormat.(string)
	schema := o.ResponseSchema
	if m, ok := o.ResponseFormat.(map[string]any); ok {
		kind = kimiMapString(m, "type")
		if schema == nil {
			schema = m["json_schema"]
			if wrapper, ok := schema.(map[string]any); ok {
				if v, ok := wrapper["schema"]; ok {
					schema = v
				} else if v, ok := wrapper["json_schema"]; ok {
					schema = v
				}
			}
		}
	}
	if kind != "json_object" && kind != "json_schema" {
		return nil
	}
	body := "The system is invoked with `response_format=" + kind + "`.\nYour response must be raw JSON data without markdown code blocks (```json) or any additional formatting."
	if kind == "json_schema" {
		encoded, err := kimiCompact(schema)
		if err != nil {
			return err
		}
		body += "\nThe JSON data must match the following schema:\n```json\n" + encoded + "\n```"
	}
	r.system("response-format", body)
	return nil
}

// RenderKimiK3Chat creates the XTML segments consumed by a reviewed K3 or K3.1
// tokenizer. String-valued function arguments retain their JSON member order.
func RenderKimiK3Chat(profile string, messages []map[string]any, options KimiChatOptions) ([]KimiChatSegment, error) {
	if err := options.resolveToolJSON(); err != nil {
		return nil, err
	}
	if profile != "kimi_k3" && profile != "kimi_k31" {
		return nil, fmt.Errorf("unsupported Kimi chat profile %q", profile)
	}
	r := kimiRenderer{profile: profile, options: options}
	if len(options.Tools) > 0 {
		if err := r.declare(options.Tools, false); err != nil {
			return nil, err
		}
	}
	thinking := options.Thinking == nil || *options.Thinking
	effort := "max"
	if options.ThinkingEffort != nil {
		effort = *options.ThinkingEffort
	}
	if thinking && effort != "" {
		switch effort {
		case "low", "high", "max":
		default:
			return nil, fmt.Errorf("unsupported thinking effort %q", effort)
		}
		r.system("thinking-effort", "`thinking_effort` guides on how much to think in your thinking channel (not including the response channel), supported values include `low`, `medium`, `high`, and `max`.\nNow the system is invoked with `thinking_effort="+effort+"`.")
	}
	for i := 0; i < len(messages); {
		end := i + 1
		run := messages[i:end]
		if messages[i]["role"] == "tool" {
			for end < len(messages) && messages[end]["role"] == "tool" {
				end++
			}
			run = kimiOrderTools(messages[i:end], r.calls)
		}
		for _, m := range run {
			if err := r.message(m, thinking); err != nil {
				return nil, fmt.Errorf("message %d: %w", i, err)
			}
			i++
		}
	}
	if err := r.format(); err != nil {
		return nil, err
	}
	if options.AddGenerationPrompt == nil || *options.AddGenerationPrompt {
		r.open("message", "role", "assistant")
		if thinking {
			r.open("think")
		} else {
			r.open("response")
		}
	}
	if options.ImagePrompts != nil && r.imageIndex != len(options.ImagePrompts) {
		return nil, fmt.Errorf("consumed %d of %d image prompts", r.imageIndex, len(options.ImagePrompts))
	}
	return r.segments, nil
}
