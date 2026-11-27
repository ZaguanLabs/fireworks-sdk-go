package sdk

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Optional pinned tokenizer snapshots used by the independent Python/Go
// oracle comparison. The ordinary offline suite uses the synthetic BPE tests.
func TestReviewedKimiSnapshots(t *testing.T) {
	root := os.Getenv("FIREWORKS_KIMI_TEST_DATA")
	if root == "" {
		t.Skip("set FIREWORKS_KIMI_TEST_DATA to the reviewed profile directories")
	}
	for _, profile := range kimiProfiles {
		t.Run(profile.name, func(t *testing.T) {
			k, err := LoadKimiTokenizer(context.Background(), filepath.Join(root, profile.name), KimiTokenizerOptions{LocalFilesOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			const msg = "[BOS]hello  café!  猫 👀  I'm OK\r\n[EOS]"
			want := []int{163584, 22931, 220, 70591, 0, 220, 220, 6927, 130732, 222, 220, 6806, 17522, 462, 163585}
			if ids := k.Encode(msg, true); !slices.Equal(ids, want) {
				t.Fatalf("IDs %v", ids)
			}
			if s, err := k.Decode(want); err != nil || s != msg {
				t.Fatal(s, err)
			}
			dir := t.TempDir()
			if err := k.SavePretrained(dir); err != nil {
				t.Fatal(err)
			}
			again, err := LoadKimiTokenizer(context.Background(), dir, KimiTokenizerOptions{LocalFilesOnly: true})
			if err != nil || again.Profile() != k.Profile() {
				t.Fatal(err)
			}
		})
	}
}

func TestKimiDataAndBPENoCheckpointCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tokenizer_config.json"), []byte(`{"auto_map":{"AutoTokenizer":["evil.DoNotRun",null]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKimiTokenizer(context.Background(), dir, KimiTokenizerOptions{}); err == nil {
		t.Fatal("accepted unreviewed config")
	}
	k := KimiTokenizer{ranks: map[string]int{"a": 0, "b": 1, "c": 2, "ab": 3, "bc": 4, "abc": 5}, tokens: []string{"a", "b", "c", "ab", "bc", "abc"}, special: kimiSpecialPattern([]string{"[BOS]"})}
	if got := k.Encode("abcabc", false); !slices.Equal(got, []int{5, 5}) {
		t.Fatal(got)
	}
	for in, want := range map[string]string{"\xe2\x82": "�", "\xff\xff": "��", "\xe0\x80\x80": "���", "😀": "😀"} {
		if got := decodeKimiUTF8([]byte(in)); got != want {
			t.Fatalf("%q -> %q", in, got)
		}
	}
}

func TestKimiK2ToolsHistoryAndMedia(t *testing.T) {
	msg := []map[string]any{{"role": "user", "content": []any{map[string]any{"type": "image_url"}}}, {"role": "assistant", "content": "a", "reasoning": "old"}}
	opts := KimiChatOptions{Tools: []map[string]any{{"type": "function", "function": map[string]any{"name": "f", "parameters": map[string]any{"type": "object", "properties": map[string]any{"z": map[string]any{"type": "integer"}, "a": map[string]any{"enum": []any{map[string]any{"z": 2, "a": 1}}}}}}}}}
	got, err := RenderKimiK2Chat("kimi_k26_v1", msg, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := "<|im_system|>tool_declare<|im_middle|># Tools\n\n## functions\nnamespace functions {\ntype f = (_: {\n  a?: {'a': 1, 'z': 2},\n  z?: number\n}) => any;\n}\n<|im_end|><|im_user|>user<|im_middle|><|media_begin|>image<|media_content|><|media_pad|><|media_end|>\n<|im_end|><|im_assistant|>assistant<|im_middle|><think></think>a<|im_end|><|im_assistant|>assistant<|im_middle|><think>"
	if got != want {
		t.Fatalf("%q", got)
	}
	opts.PreserveThinking = true
	got, _ = RenderKimiK2Chat("kimi_k26_v1", msg, opts)
	if !strings.Contains(got, "<think>old</think>") {
		t.Fatal(got)
	}
	ordered := KimiChatOptions{ToolsJSON: []byte(`[{"z":true,"a":1}]`)}
	got, err = RenderKimiK2Chat("kimi_k2_base_v1", nil, ordered)
	if err != nil || !strings.Contains(got, `[{"z": true, "a": 1}]`) {
		t.Fatal(got, err)
	}
}
