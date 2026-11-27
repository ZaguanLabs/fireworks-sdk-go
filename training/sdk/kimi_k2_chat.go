// Kimi K2 profile rendering follows the pinned Moonshot AI chat templates.
// See licenses/Kimi-K2.txt.
package sdk

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (o *KimiChatOptions) resolveToolJSON() error {
	if len(o.ToolsJSON) == 0 {
		return nil
	}
	if o.Tools != nil {
		return fmt.Errorf("supply either Tools or ToolsJSON")
	}
	return json.Unmarshal(o.ToolsJSON, &o.Tools)
}

func k2JSON(value any, compact bool) (string, error) {
	text, err := kimiCompact(value)
	if err != nil || compact {
		return text, err
	}
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	return kimiPythonValue(d)
}

func k2Content(v any, modern, base bool) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	parts, err := kimiObjects(v)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range parts {
		typ, _ := p["type"].(string)
		_, image := p["image"]
		_, url := p["image_url"]
		switch {
		case modern && (typ == "video" || typ == "video_url"):
			b.WriteString("<|kimi_k25_video_placeholder|>\n")
		case modern && (typ == "image" || typ == "image_url"):
			b.WriteString("<|media_begin|>image<|media_content|><|media_pad|><|media_end|>\n")
		case !modern && (typ == "image" || image || url):
			b.WriteString("<|media_start|>image<|media_content|><|media_pad|><|media_end|>")
			if !base {
				b.WriteByte('\n')
			}
		default:
			b.WriteString(kimiMapString(p, "text"))
		}
	}
	return b.String(), nil
}

func k2ToolCalls(calls []map[string]any) (string, error) {
	var b strings.Builder
	b.WriteString("<|tool_calls_section_begin|>")
	for _, call := range calls {
		b.WriteString("<|tool_call_begin|>" + kimiMapString(call, "id") + "<|tool_call_argument_begin|>")
		arg := kimiFunction(call)["arguments"]
		s, ok := arg.(string)
		if !ok {
			var err error
			s, err = k2JSON(arg, false)
			if err != nil {
				return "", err
			}
		}
		b.WriteString(s + "<|tool_call_end|>")
	}
	b.WriteString("<|tool_calls_section_end|>")
	return b.String(), nil
}

func RenderKimiK2Chat(profile string, messages []map[string]any, opts KimiChatOptions) (string, error) {
	if err := opts.resolveToolJSON(); err != nil {
		return "", err
	}
	p := kimiNamedProfile(profile)
	if p == nil || p.codeSHA != "" {
		return "", fmt.Errorf("invalid Kimi K2 profile %q", profile)
	}
	base := p.name == "kimi_k2_base_v1"
	instruct := strings.HasPrefix(p.name, "kimi_k2_instruct")
	modern := p.name == "kimi_k25_v1" || p.name == "kimi_k26_v1" || p.name == "kimi_k27_code_v1"
	thinking := opts.Thinking == nil || *opts.Thinking
	var b strings.Builder
	if len(opts.Tools) > 0 {
		var declaration string
		var err error
		if modern {
			declaration, err = k2ToolDeclarations(opts.Tools)
		}
		if declaration == "" || err != nil {
			if base && opts.ToolsJSON != nil {
				d := json.NewDecoder(strings.NewReader(string(opts.ToolsJSON)))
				d.UseNumber()
				declaration, err = kimiPythonValue(d)
			} else {
				declaration, err = k2JSON(opts.Tools, !base)
			}
			if err != nil {
				return "", err
			}
		}
		b.WriteString("<|im_system|>tool_declare<|im_middle|>" + declaration + "<|im_end|>")
	}
	if !modern && (len(messages) > 0 && messages[0]["role"] != "system" || len(messages) == 0 && !base && !instruct) {
		if base {
			b.WriteString("<|im_system|>system<|im_middle|>You are a helpful assistant<|im_end|>")
		} else {
			b.WriteString("<|im_system|>system<|im_middle|>You are Kimi, an AI assistant created by Moonshot AI.<|im_end|>")
			if instruct {
				b.WriteByte('\n')
			}
		}
	}
	lastAnswer := -1
	if p.name != "kimi_k27_code_v1" && !(p.name == "kimi_k26_v1" && opts.PreserveThinking) {
		for i, m := range messages {
			if m["role"] != "assistant" {
				continue
			}
			calls, err := kimiObjects(m["tool_calls"])
			if err != nil {
				return "", err
			}
			if len(calls) == 0 {
				lastAnswer = i
			}
		}
	}
	for i, m := range messages {
		role := kimiMapString(m, "role")
		name := kimiMapString(m, "name")
		if name == "" || base {
			name = role
		}
		marker := "system"
		if role == "user" || role == "assistant" {
			marker = role
		}
		if !base || role == "user" || role == "assistant" || role == "system" || role == "tool" {
			b.WriteString("<|im_" + marker + "|>" + name + "<|im_middle|>")
		}
		if role == "assistant" && !instruct && !base {
			reason := ""
			if i > lastAnswer && (p.name == "kimi_k2_thinking_v1" || p.name == "kimi_k27_code_v1" || thinking || p.name == "kimi_k26_v1" && opts.PreserveThinking) {
				reason = kimiMapString(m, "reasoning_content")
				if (p.name == "kimi_k26_v1" || p.name == "kimi_k27_code_v1") && m["reasoning"] != nil {
					reason = kimiMapString(m, "reasoning")
				}
			}
			b.WriteString("<think>" + reason + "</think>")
		}
		if role == "tool" && !base {
			b.WriteString("## Return of " + kimiMapString(m, "tool_call_id") + "\n")
		}
		content, err := k2Content(m["content"], modern, base)
		if err != nil {
			return "", err
		}
		b.WriteString(content)
		if role == "assistant" && !base {
			calls, err := kimiObjects(m["tool_calls"])
			if err != nil {
				return "", err
			}
			if len(calls) > 0 {
				text, err := k2ToolCalls(calls)
				if err != nil {
					return "", err
				}
				b.WriteString(text)
			}
		}
		b.WriteString("<|im_end|>")
	}
	if opts.AddGenerationPrompt == nil || *opts.AddGenerationPrompt {
		b.WriteString("<|im_assistant|>assistant<|im_middle|>")
		if modern {
			b.WriteString("<think>")
			if !thinking && p.name != "kimi_k27_code_v1" {
				b.WriteString("</think>")
			}
		}
	}
	return b.String(), nil
}

func (k *KimiTokenizer) EncodeChat(messages []map[string]any, opts KimiChatOptions) ([]int, error) {
	if k.profile == "kimi_k3" || k.profile == "kimi_k31" {
		segments, err := RenderKimiK3Chat(k.profile, messages, opts)
		if err != nil {
			return nil, err
		}
		var out []int
		for _, s := range segments {
			out = append(out, k.Encode(s.Text, s.AllowSpecial)...)
		}
		return out, nil
	}
	s, err := RenderKimiK2Chat(k.profile, messages, opts)
	if err != nil {
		return nil, err
	}
	var ids []int
	// The original HF reader splits registered tokens before the raw BPE
	// chunk boundaries are applied. Direct Encode is chunk-first.
	if k.registered != nil {
		for {
			pos := k.registered.FindStringIndex(s)
			if pos == nil {
				break
			}
			ids = append(ids, k.Encode(s[:pos[0]], true)...)
			ids = append(ids, k.ranks[s[pos[0]:pos[1]]])
			s = s[pos[1]:]
		}
	}
	return append(ids, k.Encode(s, true)...), nil
}

func (k *KimiTokenizer) ApplyChatTemplate(messages []map[string]string) ([]int, error) {
	values := make([]map[string]any, len(messages))
	for i, m := range messages {
		values[i] = make(map[string]any, len(m))
		for key, v := range m {
			values[i][key] = v
		}
	}
	return k.EncodeChat(values, KimiChatOptions{})
}
