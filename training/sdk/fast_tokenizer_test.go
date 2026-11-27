package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const tinyTokenizer = `{"version":"1.0","truncation":null,"padding":null,"added_tokens":[{"id":2,"content":"<s>","single_word":false,"lstrip":false,"rstrip":false,"normalized":false,"special":true}],"normalizer":null,"pre_tokenizer":{"type":"WhitespaceSplit"},"post_processor":null,"decoder":null,"model":{"type":"WordLevel","vocab":{"[UNK]":0,"hello":1,"<s>":2,"user":3},"unk_token":"[UNK]"}}`

func TestFastTokenizerSavedData(t *testing.T) {
	dir := t.TempDir()
	files := map[string][]byte{"tokenizer.json": []byte(tinyTokenizer), "tokenizer_config.json": []byte(`{"bos_token":"<s>","fix_mistral_regex":true,"auto_map":{"AutoTokenizer":["unused.PythonClass",null]}}`), "chat_template.jinja": []byte(`{{ bos_token }} {% for m in messages %}{{ m.role }} {{ m.content }}{% endfor %}`)}
	if err := cacheKimiFiles(dir, files); err != nil {
		t.Fatal(err)
	}
	tok, err := LoadTrainingTokenizer(context.Background(), dir, TokenizerOptions{LocalFilesOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	fast := tok.(*FastTokenizer)
	defer fast.Close()
	ids, err := fast.ApplyChatTemplate([]map[string]string{{"role": "user", "content": "hello"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int{2, 3, 1}) {
		t.Fatalf("ids = %v", ids)
	}
	text, err := fast.DecodeWithOptions(ids, true)
	if err != nil || text != "user hello" {
		t.Fatalf("decode = %q, %v", text, err)
	}
	save := filepath.Join(t.TempDir(), "saved")
	if err := fast.SavePretrained(save); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(save, name))
		if err != nil || string(got) != string(want) {
			t.Errorf("save %s differs: %v", name, err)
		}
	}
	if err := fast.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := fast.Encode("hello", false); err == nil {
		t.Fatal("closed tokenizer accepted input")
	}
}

func TestImmutableTokenizerSnapshot(t *testing.T) {
	const sha = "1234567890123456789012345678901234567890"
	const repo = "example/model"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/" + repo + "/revision/main":
			json.NewEncoder(w).Encode(map[string]any{"sha": sha, "siblings": []map[string]string{
				{"rfilename": "additional_chat_templates/tool_use.jinja"},
				{"rfilename": "additional_chat_templates/../../escape.jinja"},
			}})
		case "/" + repo + "/resolve/" + sha + "/additional_chat_templates/tool_use.jinja":
			w.Write([]byte("custom chat"))
		case "/" + repo + "/resolve/" + sha + "/tokenizer.json":
			w.Write([]byte(tinyTokenizer))
		case "/" + repo + "/resolve/" + sha + "/tokenizer_config.json", "/" + repo + "/resolve/" + sha + "/chat_template.jinja":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected URL %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("HF_ENDPOINT", server.URL)
	t.Setenv("HF_HUB_OFFLINE", "")
	opts := TokenizerOptions{CacheDir: t.TempDir(), HTTPClient: server.Client()}
	snap, err := hfSnapshot(context.Background(), repo, opts)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(snap, "additional_chat_templates", "tool_use.jinja")); err != nil || string(data) != "custom chat" {
		t.Fatalf("cached named template = %q, %v", data, err)
	}
	opts.LocalFilesOnly = true
	cached, err := hfSnapshot(context.Background(), repo, opts)
	if err != nil || snap != cached || !strings.HasSuffix(cached, sha) {
		t.Fatalf("cached = %q, %v", cached, err)
	}
}

// Populate FIREWORKS_TOKENIZER_ORACLES with the optional local reference
// fixture directory to exercise full-size public tokenizers without network.
func TestFastTokenizerReferenceModels(t *testing.T) {
	root := os.Getenv("FIREWORKS_TOKENIZER_ORACLES")
	if root == "" {
		t.Skip("full-size tokenizer fixtures are optional")
	}
	var cases []struct {
		Model string
		Texts []struct {
			Text    string
			IDs     []int
			Decoded string
		}
		Chats []struct {
			Messages json.RawMessage
			Options  map[string]json.RawMessage
			Text     string
			IDs      []int
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty reference fixture")
	}
	for _, tc := range cases {
		t.Run(tc.Model, func(t *testing.T) {
			tok, err := LoadFastTokenizer(context.Background(), filepath.Join(root, tc.Model), TokenizerOptions{LocalFilesOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer tok.Close()
			for _, sample := range tc.Texts {
				ids, err := tok.Encode(sample.Text, true)
				if err != nil || !reflect.DeepEqual(ids, sample.IDs) {
					t.Errorf("encode %q: got %v, want %v, err %v", sample.Text, ids, sample.IDs, err)
					continue
				}
				text, err := tok.Decode(ids)
				if err != nil || text != sample.Decoded {
					t.Errorf("decode = %q, want %q, err %v", text, sample.Decoded, err)
				}
			}
			for i, c := range tc.Chats {
				text, err := tok.RenderChatJSON(c.Messages, c.Options)
				if err != nil || text != c.Text {
					t.Errorf("chat %d: got %q, want %q, err %v", i, text, c.Text, err)
					continue
				}
				ids, err := tok.Encode(text, false)
				if err != nil || !reflect.DeepEqual(ids, c.IDs) {
					t.Errorf("chat %d token mismatch: %v", i, err)
				}
			}
		})
	}
}
