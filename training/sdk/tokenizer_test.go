package sdk

import (
	"strings"
	"testing"
)

func TestResolveTokenizerModel(t *testing.T) {
	got, err := ResolveTokenizerModel(" Qwen/Qwen3-1.7B ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Qwen/Qwen3-1.7B" {
		t.Fatalf("tokenizer model = %q", got)
	}
}

func TestResolveTokenizerModelRequiresConfiguredModel(t *testing.T) {
	_, err := ResolveTokenizerModel(" ")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != TokenizerModelRequiredMessage {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(err.Error(), "tokenizer_model") || !strings.Contains(err.Error(), "FireTitan does not resolve tokenizers server-side") {
		t.Fatalf("error = %v", err)
	}
}
