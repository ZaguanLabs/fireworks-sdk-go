package fireworks

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DatasetUploadError reports a dataset that cannot be uploaded safely.
type DatasetUploadError struct{ Message string }

func (e *DatasetUploadError) Error() string { return e.Message }

// CollectDatasetShards returns a file or sorted top-level .jsonl shards.
// Directory entries must be nonempty regular files, with no symlinks or nesting.
func CollectDatasetShards(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	check := func(name string, info os.FileInfo, directory bool) error {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("dataset shard %q must be a regular file", name)
		}
		ext := filepath.Ext(name)
		if strings.ToLower(ext) != ".jsonl" || (directory && ext != ".jsonl") {
			return fmt.Errorf("dataset shard %q must have a .jsonl extension (lowercase in directories)", name)
		}
		if info.Size() == 0 {
			return fmt.Errorf("dataset shard %q is empty", name)
		}
		return nil
	}
	if !info.IsDir() {
		if err := check(path, info, false); err != nil {
			return nil, err
		}
		return []string{path}, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	shards := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := filepath.Join(path, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if err := check(name, info, true); err != nil {
			return nil, err
		}
		shards = append(shards, name)
	}
	if len(shards) == 0 {
		return nil, fmt.Errorf("dataset directory %q contains no .jsonl files", path)
	}
	return shards, nil
}

// CountDatasetExamples counts lines without imposing a maximum JSONL record size.
func CountDatasetExamples(path string) (int64, error) {
	shards, err := CollectDatasetShards(path)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, shard := range shards {
		file, err := os.Open(shard)
		if err != nil {
			return 0, err
		}
		reader := bufio.NewReader(file)
		for {
			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				count++
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				file.Close()
				return 0, readErr
			}
		}
		if err := file.Close(); err != nil {
			return 0, err
		}
	}
	return count, nil
}

// UploadDatasetShards registers all shards once, PUTs to signed HTTPS URLs with
// a separate credential-free client, then validates the completed dataset.
func UploadDatasetShards(ctx context.Context, client *Client, datasetID, path string, opts ...RequestOption) error {
	plain := &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return uploadDatasetShards(ctx, client, datasetID, path, plain, time.Now, opts...)
}

func uploadDatasetShards(ctx context.Context, client *Client, datasetID, path string, plain *http.Client, now func() time.Time, opts ...RequestOption) error {
	if client == nil {
		return fmt.Errorf("client is required")
	}
	shards, err := CollectDatasetShards(path)
	if err != nil {
		return err
	}
	sizes := map[string]string{}
	for _, shard := range shards {
		info, err := os.Stat(shard)
		if err != nil {
			return err
		}
		sizes[filepath.Base(shard)] = strconv.FormatInt(info.Size(), 10)
	}
	dataset, err := client.Datasets.Get(ctx, datasetID, opts...)
	if err != nil {
		return err
	}
	state, exists := dataset["encryptionState"]
	if exists && state != "" && state != "ENCRYPTION_STATE_UNSPECIFIED" && state != "ENCRYPTION_STATE_PLAINTEXT" {
		return &DatasetUploadError{Message: "dataset requires encryption; refusing to upload plaintext"}
	}
	register := func() (map[string]string, error) {
		response, err := client.Datasets.GetUploadEndpoint(ctx, datasetID, map[string]any{"filenameToSize": sizes}, opts...)
		if err != nil {
			return nil, err
		}
		urls := map[string]string{}
		if raw, ok := response["filenameToSignedUrls"].(map[string]any); ok {
			for name, value := range raw {
				u, ok := value.(string)
				if !ok {
					return nil, &DatasetUploadError{"invalid signed upload URL"}
				}
				urls[name] = u
			}
		}
		if len(urls) != len(sizes) {
			return nil, &DatasetUploadError{"upload endpoint returned missing or unexpected files"}
		}
		for name := range sizes {
			u, err := url.Parse(urls[name])
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				return nil, &DatasetUploadError{fmt.Sprintf("missing or invalid HTTPS upload URL for %q", name)}
			}
		}
		return urls, nil
	}
	urls, err := register()
	if err != nil {
		return err
	}
	signedAt := now()
	for i, shard := range shards {
		if i > 0 && now().Sub(signedAt) > 45*time.Minute {
			urls, err = register()
			if err != nil {
				return err
			}
			signedAt = now()
		}
		name := filepath.Base(shard)
		file, err := os.Open(shard)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, urls[name], file)
		if err != nil {
			file.Close()
			return err
		}
		req.ContentLength, _ = strconv.ParseInt(sizes[name], 10, 64)
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Goog-Content-Length-Range", sizes[name]+","+sizes[name])
		resp, err := plain.Do(req)
		file.Close()
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &DatasetUploadError{fmt.Sprintf("upload of %q failed (HTTP %d)", name, resp.StatusCode)}
		}
		if copyErr != nil {
			return copyErr
		}
	}
	_, err = client.Datasets.ValidateUpload(ctx, datasetID, map[string]any{}, opts...)
	return err
}
