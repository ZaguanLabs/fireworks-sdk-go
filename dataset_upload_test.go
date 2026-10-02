package fireworks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDatasetShardPolicy(t *testing.T) {
	dir := t.TempDir()
	if _, err := CollectDatasetShards(dir); err == nil {
		t.Fatal("empty directory accepted")
	}
	for _, name := range []string{"b.jsonl", "a.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	shards, err := CollectDatasetShards(dir)
	if err != nil || filepath.Base(shards[0]) != "a.jsonl" {
		t.Fatalf("%v %v", shards, err)
	}
	count, err := CountDatasetExamples(dir)
	if err != nil || count != 4 {
		t.Fatalf("%d %v", count, err)
	}
	if err := os.Symlink(shards[0], filepath.Join(dir, "link.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := CollectDatasetShards(dir); err == nil {
		t.Fatal("symlink accepted")
	}
	for _, name := range []string{"empty.jsonl", "bad.txt", "upper.JSONL"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			data := []byte("{}")
			if strings.HasPrefix(name, "empty") {
				data = nil
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := CollectDatasetShards(dir); err == nil {
				t.Fatal("bad directory accepted")
			}
		})
	}
}

func TestDatasetUploadSignedURLs(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.jsonl", "b.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	uploaded := 0
	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("credentials or wrong method: %v", r.Header)
		}
		if r.Header.Get("X-Goog-Content-Length-Range") != "3,3" || r.Header.Get("Content-Type") != "application/octet-stream" {
			t.Error(r.Header)
		}
		data, _ := io.ReadAll(r.Body)
		if string(data) != "{}\n" {
			t.Errorf("body %q", data)
		}
		uploaded++
	}))
	defer storage.Close()
	for _, mode := range []string{"ok", "encrypted", "missing", "extra", "insecure"} {
		t.Run(mode, func(t *testing.T) {
			uploaded = 0
			registrations := 0
			validated := false
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, ":getUploadEndpoint"):
					registrations++
					var body map[string]map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body["filenameToSize"]) != 2 {
						t.Errorf("must register all shards: %v", body)
					}
					urls := map[string]string{"a.jsonl": storage.URL + "/a", "b.jsonl": storage.URL + "/b"}
					if mode == "missing" {
						delete(urls, "b.jsonl")
					}
					if mode == "extra" {
						urls["x.jsonl"] = storage.URL
					}
					if mode == "insecure" {
						urls["a.jsonl"] = "http://example.test/a"
					}
					json.NewEncoder(w).Encode(map[string]any{"filenameToSignedUrls": urls})
				case strings.HasSuffix(r.URL.Path, ":validateUpload"):
					validated = true
					io.WriteString(w, `{}`)
				default:
					state := "ENCRYPTION_STATE_PLAINTEXT"
					if mode == "encrypted" {
						state = "ENCRYPTION_STATE_CMEK"
					}
					json.NewEncoder(w).Encode(map[string]any{"encryptionState": state})
				}
			}))
			defer api.Close()
			client, err := NewClient(WithAPIKey("private-key"), WithBaseURL(api.URL), WithDefaultAccountID("acct"))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			ticks := 0
			err = uploadDatasetShards(context.Background(), client, "ds", dir, storage.Client(), func() time.Time { ticks++; return now.Add(time.Duration(ticks) * 46 * time.Minute) })
			if mode == "ok" {
				if err != nil || uploaded != 2 || registrations != 2 || !validated {
					t.Fatalf("err %v uploaded %d registrations %d validated %v", err, uploaded, registrations, validated)
				}
			} else {
				var safety *DatasetUploadError
				if !errors.As(err, &safety) || uploaded != 0 || validated {
					t.Fatalf("err %v uploaded %d validated %v", err, uploaded, validated)
				}
			}
		})
	}
}
