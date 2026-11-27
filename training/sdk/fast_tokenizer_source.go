package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var hfComponent = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*$`)
var hfCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

func hfSnapshot(ctx context.Context, source string, o KimiTokenizerOptions) (string, error) {
	if st, err := os.Stat(source); err == nil && st.IsDir() {
		return source, nil
	}
	parts := strings.Split(source, "/")
	if len(parts) > 2 || len(parts) == 0 {
		return "", fmt.Errorf("invalid tokenizer repository %q", source)
	}
	for _, s := range parts {
		if !hfComponent.MatchString(s) {
			return "", fmt.Errorf("invalid tokenizer repository %q", source)
		}
	}
	rev := o.Revision
	if rev == "" {
		rev = "main"
	}
	if !hfComponent.MatchString(rev) {
		return "", fmt.Errorf("invalid tokenizer revision %q", rev)
	}
	repo := filepath.Join(kimiHubDir(o.CacheDir), "models--"+strings.Join(parts, "--"))
	pin := rev
	if !hfCommit.MatchString(pin) {
		if data, e := os.ReadFile(filepath.Join(repo, "refs", rev)); e == nil {
			pin = strings.TrimSpace(string(data))
		}
	}
	offline := o.LocalFilesOnly || truthySetting(os.Getenv("HF_HUB_OFFLINE"))
	if hfCommit.MatchString(pin) {
		dir := filepath.Join(repo, "snapshots", pin)
		if _, e := os.Stat(filepath.Join(dir, "tokenizer.json")); e == nil && (offline || pin == rev) {
			return dir, nil
		}
	}
	if offline {
		return "", fmt.Errorf("tokenizer %s revision %s is absent from the local cache", source, rev)
	}
	client := o.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := strings.TrimRight(os.Getenv("HF_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = "https://huggingface.co"
	}
	parsed, e := url.Parse(endpoint)
	if e != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid Hugging Face endpoint")
	}
	files := map[string][]byte{}
	revision, names, e := hfTokenizerFiles(ctx, client, endpoint, source, rev)
	if e != nil {
		return "", e
	}
	for _, name := range names {
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/"+source+"/resolve/"+revision+"/"+name, nil)
		if e != nil {
			return "", e
		}
		response, e := client.Do(req)
		if e != nil {
			return "", e
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			if name != "tokenizer.json" && response.StatusCode == 404 {
				continue
			}
			return "", fmt.Errorf("download %s: HTTP %d", name, response.StatusCode)
		}
		if name == "tokenizer.json" {
			resolved := response.Header.Get("X-Repo-Commit")
			if resolved == "" && hfCommit.MatchString(revision) {
				resolved = revision
			}
			if !hfCommit.MatchString(resolved) {
				response.Body.Close()
				return "", fmt.Errorf("tokenizer response did not identify its repository commit")
			}
			if hfCommit.MatchString(revision) && revision != resolved {
				response.Body.Close()
				return "", fmt.Errorf("tokenizer revision mismatch")
			}
			revision = resolved
		}
		limit := int64(256 << 20)
		if name != "tokenizer.json" {
			limit = 16 << 20
		}
		raw, e := io.ReadAll(io.LimitReader(response.Body, limit+1))
		response.Body.Close()
		if e != nil {
			return "", e
		}
		if int64(len(raw)) > limit {
			return "", fmt.Errorf("oversized %s", name)
		}
		files[name] = raw
	}
	dir := filepath.Join(repo, "snapshots", revision)
	// Publish the principal tokenizer last so offline readers see complete new snapshots.
	main := files["tokenizer.json"]
	delete(files, "tokenizer.json")
	if e := cacheKimiFiles(dir, files); e != nil {
		return "", e
	}
	if e := cacheKimiFiles(dir, map[string][]byte{"tokenizer.json": main}); e != nil {
		return "", e
	}
	if !hfCommit.MatchString(rev) {
		refs := filepath.Join(repo, "refs")
		if e = os.MkdirAll(refs, 0755); e != nil {
			return "", e
		}
		tmp, e := os.CreateTemp(refs, ".revision-")
		if e != nil {
			return "", e
		}
		defer os.Remove(tmp.Name())
		_, err := io.WriteString(tmp, revision)
		if e = errors.Join(err, tmp.Close()); e != nil {
			return "", e
		}
		if e = os.Rename(tmp.Name(), filepath.Join(refs, rev)); e != nil {
			return "", e
		}
	}
	return dir, nil
}

func hfTokenizerFiles(ctx context.Context, client *http.Client, endpoint, source, rev string) (string, []string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/models/"+source+"/revision/"+rev, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("resolve tokenizer revision: HTTP %d", resp.StatusCode)
	}
	var info struct {
		SHA      string `json:"sha"`
		Siblings []struct {
			Filename string `json:"rfilename"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&info); err != nil {
		return "", nil, err
	}
	if !hfCommit.MatchString(info.SHA) || (hfCommit.MatchString(rev) && rev != info.SHA) {
		return "", nil, fmt.Errorf("repository returned an invalid commit for %s", rev)
	}
	names := []string{"tokenizer.json", "tokenizer_config.json", "chat_template.jinja"}
	for _, file := range info.Siblings {
		name, ok := strings.CutPrefix(file.Filename, "additional_chat_templates/")
		if ok && hfComponent.MatchString(name) && strings.HasSuffix(name, ".jinja") {
			names = append(names, file.Filename)
		}
	}
	return info.SHA, names, nil
}
