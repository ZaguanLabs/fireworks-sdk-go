package sdk

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ByteLevel serializes non-printable bytes as Unicode scalars starting at 256.
func kimiByteDecoder() map[rune]byte {
	decoder := make(map[rune]byte, 256)
	next := rune(256)
	for i := 0; i < 256; i++ {
		r := rune(i)
		if !(i >= 33 && i <= 126 || i >= 161 && i <= 172 || i >= 174) {
			r = next
			next++
		}
		decoder[r] = byte(i)
	}
	return decoder
}

func readSavedKimiFast(dir string, profile *kimiProfile, config []byte) (*KimiTokenizer, error) {
	// Raw source material always goes through the pinned raw reader.
	if _, err := os.Stat(filepath.Join(dir, "tiktoken.model")); err == nil {
		return nil, fmt.Errorf("unverified raw Kimi config in fast bundle")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	var saved struct {
		Model struct {
			Type  string
			Vocab map[string]int
		}
		Added []struct {
			ID      int
			Content string
			Special bool
		} `json:"added_tokens"`
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	if saved.Model.Type != "BPE" || len(saved.Model.Vocab) != kimiBaseVocabSize {
		return nil, fmt.Errorf("saved bundle does not contain the reviewed Kimi BPE vocabulary")
	}
	tokens := make([]string, kimiBaseVocabSize+kimiReservedTokens)
	byteDecoder := kimiByteDecoder()
	for token, id := range saved.Model.Vocab {
		if id < 0 || id >= kimiBaseVocabSize {
			return nil, fmt.Errorf("invalid Kimi BPE rank %d", id)
		}
		data := make([]byte, 0, len(token))
		for _, char := range token {
			b, ok := byteDecoder[char]
			if !ok {
				return nil, fmt.Errorf("invalid ByteLevel token")
			}
			data = append(data, b)
		}
		tokens[id] = string(data)
	}
	digest := sha256.New()
	for id, token := range tokens[:kimiBaseVocabSize] {
		fmt.Fprintf(digest, "%s %d\n", base64.StdEncoding.EncodeToString([]byte(token)), id)
	}
	if fmt.Sprintf("%x", digest.Sum(nil)) != kimiVocabSHA {
		return nil, fmt.Errorf("saved token ranks differ from the reviewed Kimi vocabulary")
	}
	for _, item := range saved.Added {
		if item.ID < kimiBaseVocabSize || item.ID >= len(tokens) || !item.Special {
			return nil, fmt.Errorf("invalid saved special token %q", item.Content)
		}
		tokens[item.ID] = item.Content
	}
	return newKimiTokenizer(profile, tokens, map[string][]byte{"tokenizer.json": raw, "tokenizer_config.json": config})
}
