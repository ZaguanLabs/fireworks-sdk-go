package sdk

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed internal/hftokenizer/tokenizer.wasm
var fastTokenizerWASM []byte

var tokenizerCodeCache = sync.OnceValue(wazero.NewCompilationCache)

// FastTokenizer runs Hugging Face tokenizers in an isolated WebAssembly instance.
// Templates and vocabulary are supplied as data and the guest has no filesystem.
type FastTokenizer struct {
	mu        sync.Mutex
	rt        wazero.Runtime
	mod       api.Module
	files     map[string][]byte
	config    map[string]any
	templates map[string]string
}

// TokenizerOptions controls Hub snapshot selection and local cache use.
type TokenizerOptions = KimiTokenizerOptions

// LoadFastTokenizer opens a local saved tokenizer or a Hugging Face repository.
// Close the returned tokenizer when finished to release its WebAssembly memory.
func LoadFastTokenizer(ctx context.Context, source string, options TokenizerOptions) (*FastTokenizer, error) {
	dir, err := hfSnapshot(ctx, source, options)
	if err != nil {
		return nil, err
	}
	return loadFastTokenizerDir(ctx, dir)
}

func loadFastTokenizerDir(ctx context.Context, dir string) (*FastTokenizer, error) {
	var err error
	files := map[string][]byte{}
	for _, name := range []string{"tokenizer.json", "tokenizer_config.json", "chat_template.jinja"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			if name != "tokenizer.json" && errors.Is(e, os.ErrNotExist) {
				continue
			}
			return nil, e
		}
		files[name] = b
	}
	config := map[string]any{}
	if raw := files["tokenizer_config.json"]; raw != nil {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
	}
	templates := map[string]string{}
	switch v := config["chat_template"].(type) {
	case string:
		templates["default"] = v
	case map[string]any:
		for name, item := range v {
			if text, ok := item.(string); ok {
				templates[name] = text
			}
		}
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if text, ok := m["template"].(string); ok {
					if name, ok := m["name"].(string); ok {
						templates[name] = text
					}
				}
			}
		}
	}
	if text := files["chat_template.jinja"]; len(text) > 0 {
		templates["default"] = string(text)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "additional_chat_templates"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".jinja") {
			continue
		}
		relative := filepath.Join("additional_chat_templates", name)
		text, err := os.ReadFile(filepath.Join(dir, relative))
		if err != nil {
			return nil, err
		}
		templates[strings.TrimSuffix(name, ".jinja")] = string(text)
		files[relative] = text
	}
	cfg := wazero.NewRuntimeConfig().WithCompilationCache(tokenizerCodeCache()).WithMemoryLimitPages(16384).WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	t := &FastTokenizer{rt: rt, files: files, config: config, templates: templates}
	success := false
	defer func() {
		if !success {
			_ = rt.Close(context.Background())
		}
	}()
	if _, err = wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return nil, err
	}
	binary, err := rt.CompileModule(ctx, fastTokenizerWASM)
	if err != nil {
		return nil, fmt.Errorf("compile tokenizer runtime: %w", err)
	}
	t.mod, err = rt.InstantiateModule(ctx, binary, wazero.NewModuleConfig().WithName("").WithRandSource(rand.Reader).WithStartFunctions())
	if err != nil {
		return nil, fmt.Errorf("start tokenizer runtime: %w", err)
	}
	if init := t.mod.ExportedFunction("_initialize"); init != nil {
		if _, err := init.Call(ctx); err != nil {
			return nil, err
		}
	}
	if err = t.invoke(ctx, 1, files["tokenizer.json"], nil); err != nil {
		return nil, err
	}
	success = true
	runtime.SetFinalizer(t, func(tokenizer *FastTokenizer) { _ = tokenizer.Close() })
	return t, nil
}

func (t *FastTokenizer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	runtime.SetFinalizer(t, nil)
	if t.rt == nil {
		return nil
	}
	err := t.rt.Close(context.Background())
	t.rt = nil
	t.mod = nil
	return err
}

func (t *FastTokenizer) invoke(ctx context.Context, op uint32, input []byte, dest any) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.mod == nil || t.mod.IsClosed() {
		return errors.New("tokenizer is closed")
	}
	// Wazero terminates a guest whose context expires. This also bounds invalid
	// templates and heavy tokenizer definitions when a caller supplies no deadline.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if len(input) > 256<<20 {
		return errors.New("tokenizer input exceeds 256 MiB")
	}
	alloc, free, invoke := t.mod.ExportedFunction("alloc"), t.mod.ExportedFunction("dealloc"), t.mod.ExportedFunction("invoke")
	if alloc == nil || free == nil || invoke == nil {
		return errors.New("incompatible tokenizer guest ABI")
	}
	n := uint64(len(input))
	buffer, err := alloc.Call(ctx, n)
	if err != nil {
		return err
	}
	ptr := uint32(buffer[0])
	defer free.Call(ctx, uint64(ptr), n)
	if !t.mod.Memory().Write(ptr, input) {
		return errors.New("invalid guest input buffer")
	}
	result, err := invoke.Call(ctx, uint64(op), uint64(ptr), n)
	if err != nil {
		return err
	}
	address, size := uint32(result[0]>>32), uint32(result[0])
	defer free.Call(ctx, uint64(address), uint64(size))
	data, ok := t.mod.Memory().Read(address, size)
	if !ok {
		return errors.New("invalid guest result buffer")
	}
	var envelope struct {
		OK    json.RawMessage `json:"ok"`
		Error string          `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode tokenizer response: %w", err)
	}
	if envelope.Error != "" {
		return errors.New(envelope.Error)
	}
	if envelope.OK == nil {
		return errors.New("tokenizer response missing result")
	}
	if dest != nil {
		return json.Unmarshal(envelope.OK, dest)
	}
	return nil
}
func (t *FastTokenizer) call(op uint32, input, dest any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return t.invoke(context.Background(), op, body, dest)
}

func (t *FastTokenizer) Encode(text string, addSpecialTokens bool) ([]int, error) {
	var result []int
	err := t.call(2, map[string]any{"text": text, "add_special_tokens": addSpecialTokens}, &result)
	return result, err
}
func (t *FastTokenizer) Decode(ids []int) (string, error) { return t.DecodeWithOptions(ids, false) }
func (t *FastTokenizer) DecodeWithOptions(ids []int, skipSpecialTokens bool) (string, error) {
	if ids == nil {
		ids = []int{}
	}
	var result string
	err := t.call(3, map[string]any{"ids": ids, "skip_special_tokens": skipSpecialTokens}, &result)
	return result, err
}

// RenderChat renders the saved template with Transformers' special-token
// variables and caller options such as tools, reasoning_effort and thinking_mode.
func (t *FastTokenizer) RenderChat(messages []map[string]any, options map[string]any) (string, error) {
	if messages == nil {
		messages = []map[string]any{}
	}
	return t.renderChat(messages, options)
}

// RenderChatJSON accepts ordered JSON for tool declarations and message objects.
// Use it when prompt tokens must reproduce an existing JSON document's key order.
func (t *FastTokenizer) RenderChatJSON(messages json.RawMessage, options map[string]json.RawMessage) (string, error) {
	if !json.Valid(messages) {
		return "", errors.New("invalid message JSON")
	}
	vars := make(map[string]any, len(options))
	for k, raw := range options {
		if k == "chat_template" {
			var name string
			if err := json.Unmarshal(raw, &name); err != nil {
				return "", err
			}
			vars[k] = name
		} else if k == "tools" && string(raw) == "null" {
			vars[k] = nil
		} else {
			vars[k] = raw
		}
	}
	return t.renderChat(messages, vars)
}

func (t *FastTokenizer) renderChat(messages any, options map[string]any) (string, error) {
	name := "default"
	if tools := options["tools"]; tools != nil && t.templates["tool_use"] != "" {
		name = "tool_use"
	}
	if override, ok := options["chat_template"].(string); ok {
		name = override
	}
	template := t.templates[name]
	if template == "" {
		return "", fmt.Errorf("no saved chat template named %q", name)
	}
	vars := map[string]any{"messages": messages, "add_generation_prompt": true}
	for _, key := range []string{"bos_token", "eos_token", "unk_token", "sep_token", "pad_token", "cls_token", "mask_token", "additional_special_tokens"} {
		switch token := t.config[key].(type) {
		case string:
			vars[key] = token
		case map[string]any:
			vars[key] = token["content"]
		case []any:
			vars[key] = token
		}
	}
	for k, v := range options {
		if k != "chat_template" && k != "messages" {
			vars[k] = v
		}
	}
	var text string
	err := t.call(4, map[string]any{"template": template, "context": vars, "now": time.Now().Format(time.RFC3339Nano)}, &text)
	return text, err
}

func (t *FastTokenizer) ApplyChatTemplate(messages []map[string]string) ([]int, error) {
	m := make([]map[string]any, len(messages))
	for i, msg := range messages {
		m[i] = make(map[string]any, len(msg))
		for k, v := range msg {
			m[i][k] = v
		}
	}
	text, err := t.RenderChat(m, nil)
	if err != nil {
		return nil, err
	}
	return t.Encode(text, false)
}

// SavePretrained copies the original data files, including tokenizer settings
// such as fix_mistral_regex and the model's declared chat template.
func (t *FastTokenizer) SavePretrained(dir string) error {
	for name := range t.files {
		if filepath.Dir(name) != "." {
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0755); err != nil {
				return err
			}
		}
	}
	return cacheKimiFiles(dir, t.files)
}

func LoadTrainingTokenizer(ctx context.Context, source string, opts TokenizerOptions) (DeploymentTokenizer, error) {
	if kimiNamedProfile(source) != nil {
		return LoadKimiTokenizer(ctx, source, opts)
	}
	dir, err := hfSnapshot(ctx, source, opts)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tokenizer_config.json"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if raw != nil {
		var config kimiConfig
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
		if config.K2 != "" || config.K3 != "" || config.Class == "TikTokenTokenizer" || kimiNamedProfile(opts.BaseModel) != nil {
			return LoadKimiTokenizer(ctx, dir, opts)
		}
	}
	return loadFastTokenizerDir(ctx, dir)
}
