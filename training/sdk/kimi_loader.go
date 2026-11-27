package sdk

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const kimiBaseVocabSize = 163584
const kimiReservedTokens = 256

type KimiTokenizerOptions struct {
	// BaseModel can supply the intended profile for an opaque source path.
	BaseModel      string
	Revision       string
	LocalFilesOnly bool
	// CacheDir follows Hugging Face's Hub directory layout.
	CacheDir   string
	HTTPClient *http.Client
}

func kimiNamedProfile(name string) *kimiProfile {
	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "/"))
	if name == "moonshotai/kimi-k2.5-text-only" {
		name = "kimi-k2p5"
	}
	for i := range kimiProfiles {
		p := &kimiProfiles[i]
		if name == strings.ToLower(p.repository) || name == p.model || name == "accounts/fireworks/models/"+p.model || name == p.name {
			return p
		}
	}
	return nil
}

func kimiSHA(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func kimiHubDir(configured string) string {
	if configured != "" {
		return configured
	}
	if dir := os.Getenv("HF_HUB_CACHE"); dir != "" {
		return dir
	}
	if dir := os.Getenv("HF_HOME"); dir != "" {
		return filepath.Join(dir, "hub")
	}
	dir, _ := os.UserCacheDir()
	return filepath.Join(dir, "huggingface", "hub")
}

type kimiConfig struct {
	Class string `json:"tokenizer_class"`
	K2    string `json:"fireworks_kimi_k2_profile"`
	K3    string `json:"kimi_native_profile"`
	Added map[int]struct {
		Content string `json:"content"`
	} `json:"added_tokens_decoder"`
}

// LoadKimiTokenizer opens verified raw tokenizer data, an offline pinned HF
// snapshot or a compatible saved bundle. Only data enters the Go tokenizer.
func LoadKimiTokenizer(ctx context.Context, source string, opts KimiTokenizerOptions) (*KimiTokenizer, error) {
	if info, err := os.Stat(source); err == nil && info.IsDir() {
		return readKimiDir(source, kimiNamedProfile(opts.BaseModel), false)
	}
	p := kimiNamedProfile(source)
	if p == nil {
		return nil, fmt.Errorf("unrecognized Kimi tokenizer source %q", source)
	}
	rev := opts.Revision
	if rev == "" {
		rev = p.revision
	}
	if rev != p.revision && !(p.name == "kimi_k2_thinking_v1" && rev == "a51ccc050d73dab088bf7b0e2dd9b30ae85a4e55") {
		return nil, fmt.Errorf("unreviewed revision for %s", p.repository)
	}
	dir := filepath.Join(kimiHubDir(opts.CacheDir), "models--"+strings.ReplaceAll(p.repository, "/", "--"), "snapshots", rev)
	tok, err := readKimiDir(dir, p, true)
	if err == nil {
		return tok, nil
	}
	offline := strings.ToLower(os.Getenv("HF_HUB_OFFLINE"))
	if opts.LocalFilesOnly || offline == "1" || offline == "true" || offline == "on" || offline == "yes" || !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	files := map[string][]byte{}
	pins := map[string]string{"tiktoken.model": kimiVocabSHA, "tokenizer_config.json": p.configSHA}
	if p.templateSHA != "" {
		pins["chat_template.jinja"] = p.templateSHA
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	for name, digest := range pins {
		url := "https://huggingface.co/" + p.repository + "/resolve/" + rev + "/" + name
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		response.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if response.StatusCode != http.StatusOK || kimiSHA(data) != digest {
			return nil, fmt.Errorf("unable to verify %s for %s (HTTP %d)", name, p.repository, response.StatusCode)
		}
		files[name] = data
	}
	if err := cacheKimiFiles(dir, files); err != nil {
		return nil, err
	}
	return readKimiDir(dir, p, true)
}

// SavePretrained writes the verified tokenizer data so LoadKimiTokenizer can
// reopen the profile offline. Existing files must agree byte for byte.
func (k *KimiTokenizer) SavePretrained(dir string) error { return cacheKimiFiles(dir, k.rawFiles) }

func cacheKimiFiles(dir string, files map[string][]byte) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for name, data := range files {
		target := filepath.Join(dir, name)
		old, err := os.ReadFile(target)
		if err == nil {
			if !bytes.Equal(data, old) {
				return fmt.Errorf("conflicting tokenizer file %s", target)
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, ".kimi-")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		_, err = tmp.Write(data)
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Link(tmpName, target)
			if errors.Is(err, os.ErrExist) {
				present, readErr := os.ReadFile(target)
				if readErr == nil && bytes.Equal(data, present) {
					err = nil
				}
			}
		}
		_ = os.Remove(tmpName)
		if err != nil {
			return err
		}
	}
	return nil
}

func readKimiDir(dir string, hint *kimiProfile, official bool) (*KimiTokenizer, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "tokenizer_config.json"))
	if err != nil {
		return nil, err
	}
	var cfg kimiConfig
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	template, err := os.ReadFile(filepath.Join(dir, "chat_template.jinja"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	templateSHA := ""
	if err == nil {
		templateSHA = kimiSHA(template)
	}
	configSHA := kimiSHA(raw)
	var profile *kimiProfile
	for i := range kimiProfiles {
		p := &kimiProfiles[i]
		if p.configSHA == configSHA && p.templateSHA == templateSHA {
			profile = p
			break
		}
	}
	if !official && profile == nil && cfg.K2 != "" {
		profile = kimiNamedProfile(cfg.K2)
	}
	if profile == nil && cfg.K3 != "" && !official {
		if p := kimiNamedProfile(cfg.K3); p != nil && p.codeSHA != "" && (hint == nil || hint == p) {
			return readSavedKimiFast(dir, p, raw)
		}
	}
	if hint != nil && (official || profile == nil) {
		profile = hint
	}
	if profile == nil {
		return nil, fmt.Errorf("Kimi profile is missing or unreviewed")
	}
	if (cfg.K2 != "" && cfg.K2 != profile.name) || (cfg.K3 != "" && cfg.K3 != profile.name) {
		return nil, fmt.Errorf("conflicting Kimi profile declarations")
	}
	if cfg.Class != "TikTokenTokenizer" {
		return nil, fmt.Errorf("unreviewed tokenizer class %q", cfg.Class)
	}
	if official || cfg.K2 == "" {
		if configSHA != profile.configSHA || templateSHA != profile.templateSHA {
			return nil, fmt.Errorf("Kimi config/template differs from reviewed %s data", profile.name)
		}
	}
	if profile.codeSHA != "" {
		provenance, err := os.ReadFile(filepath.Join(dir, "tokenization_kimi.py"))
		if err == nil && kimiSHA(provenance) != profile.codeSHA {
			return nil, fmt.Errorf("unreviewed Kimi provenance file")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	vocab, err := os.ReadFile(filepath.Join(dir, "tiktoken.model"))
	if err != nil {
		return nil, err
	}
	if kimiSHA(vocab) != kimiVocabSHA {
		return nil, fmt.Errorf("unreviewed Kimi vocabulary")
	}
	files := map[string][]byte{"tokenizer_config.json": raw, "tiktoken.model": vocab}
	if template != nil {
		files["chat_template.jinja"] = template
	}
	tokens := make([]string, kimiBaseVocabSize+kimiReservedTokens)
	lines := bufio.NewScanner(bytes.NewReader(vocab))
	for lines.Scan() {
		fields := strings.Fields(lines.Text())
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid vocabulary row")
		}
		b, err := base64.StdEncoding.DecodeString(fields[0])
		if err != nil {
			return nil, err
		}
		id, err := strconv.Atoi(fields[1])
		if err != nil || id < 0 || id >= kimiBaseVocabSize {
			return nil, fmt.Errorf("invalid vocabulary ID")
		}
		tokens[id] = string(b)
	}
	if err := lines.Err(); err != nil {
		return nil, err
	}
	for id := kimiBaseVocabSize; id < len(tokens); id++ {
		n := cfg.Added[id].Content
		if n == "" {
			n = fmt.Sprintf("<|reserved_token_%d|>", id)
		}
		tokens[id] = n
	}
	tokenizer, err := newKimiTokenizer(profile, tokens, files)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(cfg.Added))
	for _, token := range cfg.Added {
		if token.Content != "" {
			names = append(names, token.Content)
		}
	}
	if len(names) > 0 {
		tokenizer.registered = kimiSpecialPattern(names)
	}
	return tokenizer, nil
}

func newKimiTokenizer(profile *kimiProfile, tokens []string, files map[string][]byte) (*KimiTokenizer, error) {
	ranks := make(map[string]int, len(tokens))
	for i, text := range tokens {
		if text == "" {
			return nil, fmt.Errorf("empty Kimi token %d", i)
		}
		if _, exists := ranks[text]; exists {
			return nil, fmt.Errorf("duplicate Kimi token %d", i)
		}
		ranks[text] = i
	}
	return &KimiTokenizer{profile: profile.name, ranks: ranks, tokens: tokens, special: kimiSpecialPattern(append([]string(nil), tokens[kimiBaseVocabSize:]...)), rawFiles: files}, nil
}

// GetTokenizer opens the configured training tokenizer. Close a returned
// *FastTokenizer when its deployment no longer uses it.
func (c *FiretitanTrainingClient) GetTokenizer(ctx context.Context, opts TokenizerOptions) (DeploymentTokenizer, error) {
	source, err := c.GetTokenizerModel()
	if err != nil {
		return nil, err
	}
	if opts.BaseModel == "" {
		opts.BaseModel = c.Config.BaseModel
	}
	return LoadTrainingTokenizer(ctx, source, opts)
}
