// MLflow mirrors training metrics to the configured Fireworks ingest proxy.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type MLflowOptions struct {
	Project, RunName string
	ServerRequired   bool
	Tags             map[string]string
	Params           map[string]any
	Transport        http.RoundTripper
}

type mlflowBatch struct {
	kind    string
	entries []map[string]any
}

// MLflowIngestClient posts queued metrics, immutable parameters and tags.
// Call Finish at the end of the run to drain queued writes and record its status.
type MLflowIngestClient struct {
	ExperimentID, RunID string
	endpoint, token     string
	http                *http.Client
	mu                  sync.Mutex
	queue               chan mlflowBatch
	done                chan struct{}
	params              map[string]bool
	status              string
	closed              bool
	err                 error
}

func mlflowFile(dir, name string) string {
	p := filepath.Join(dir, name)
	st, err := os.Lstat(p)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 4096 {
		return ""
	}
	f, err := os.OpenFile(p, os.O_RDONLY|mlflowReadFlags, 0)
	if err != nil {
		return ""
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(st, opened) {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(data) > 4096 || !utf8.Valid(data) {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func mlflowEnv(k string) string { return strings.TrimSpace(os.Getenv(k)) }
func mlflowFirst(values ...string) string {
	for _, s := range values {
		if s != "" {
			return s
		}
	}
	return ""
}
func truthySetting(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func MLflowRequiredByServer(h http.Header) bool {
	for k, values := range h {
		if strings.EqualFold(k, "x-fireworks-require-mlflow") {
			for _, v := range values {
				if truthySetting(v) {
					return true
				}
			}
		}
	}
	return false
}

// NewMLflowFromEnv opens a write-only run when credentials are configured or
// required. It returns (nil, nil) for an unconfigured optional mirror or an
// explicit FIREWORKS_SKIP_MLFLOW_REASON.
func NewMLflowFromEnv(ctx context.Context, o MLflowOptions) (*MLflowIngestClient, error) {
	if mlflowEnv("FIREWORKS_SKIP_MLFLOW_REASON") != "" {
		return nil, nil
	}
	dir := mlflowEnv("FIREWORKS_MLFLOW_CONFIG_DIR")
	if dir == "" {
		if root, e := os.UserConfigDir(); e == nil {
			dir = filepath.Join(root, "mlflow")
		}
	}
	endpoint, token := mlflowEnv("MLFLOW_TRACKING_URI"), mlflowEnv("MLFLOW_TRACKING_TOKEN")
	if dir != "" {
		if endpoint == "" {
			endpoint = mlflowFile(dir, "tracking_uri")
		}
		if token == "" {
			token = mlflowFile(dir, "token")
		}
	}
	if endpoint == "" && token == "" && !o.ServerRequired && !truthySetting(mlflowEnv("FIREWORKS_REQUIRE_MLFLOW")) {
		return nil, nil
	}
	u, e := url.Parse(endpoint)
	if e != nil || u.Hostname() == "" || token == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("MLflow tracking URI or token is incomplete or invalid")
	}
	loopback := strings.EqualFold(u.Hostname(), "localhost") || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("MLflow tracking URI must use HTTPS or loopback HTTP")
	}
	transport := o.Transport
	if transport == nil {
		transport = &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, IdleConnTimeout: 90 * time.Second}
	}
	c := &MLflowIngestClient{endpoint: strings.TrimRight(endpoint, "/"), token: token, http: &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, queue: make(chan mlflowBatch, 1024), done: make(chan struct{}), params: make(map[string]bool)}
	project := mlflowFirst(mlflowEnv("MLFLOW_EXPERIMENT_NAME"), o.Project, "fireworks-training")
	var exp struct {
		ID json.RawMessage `json:"experiment_id"`
	}
	if err := c.post(ctx, "/ingest/experiments", map[string]any{"name": project}, &exp); err != nil {
		c.http.CloseIdleConnections()
		return nil, err
	}
	c.ExperimentID = mlflowID(exp.ID)
	if c.ExperimentID == "" {
		c.http.CloseIdleConnections()
		return nil, fmt.Errorf("MLflow experiment response missing experiment_id")
	}
	tags := map[string]string{"fireworks.source": "training-sdk"}
	for k, v := range o.Tags {
		tags[k] = v
	}
	runName := mlflowFirst(mlflowEnv("MLFLOW_RUN_NAME"), o.RunName, mlflowEnv("FIREWORKS_JOB_ID"), time.Now().UTC().Format("fw-run-20060102T150405Z"))
	var created struct {
		Run struct {
			Info struct {
				ID json.RawMessage `json:"run_id"`
			} `json:"info"`
		} `json:"run"`
	}
	err := c.post(ctx, "/api/2.0/mlflow/runs/create", map[string]any{"experiment_id": c.ExperimentID, "run_name": runName, "start_time": time.Now().UnixMilli(), "tags": mlflowTags(tags)}, &created)
	if err != nil {
		c.http.CloseIdleConnections()
		return nil, err
	}
	c.RunID = mlflowID(created.Run.Info.ID)
	if c.RunID == "" {
		c.http.CloseIdleConnections()
		return nil, fmt.Errorf("MLflow run response missing run_id")
	}
	go c.send()
	c.LogParams(o.Params)
	return c, nil
}

func mlflowID(raw json.RawMessage) string {
	var id string
	if json.Unmarshal(raw, &id) == nil {
		return id
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}
func (c *MLflowIngestClient) post(ctx context.Context, route string, value any, out any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+route, bytes.NewReader(data))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		if (resp.StatusCode == 429 || resp.StatusCode == 503) && attempt < 2 {
			resp.Body.Close()
			seconds := 1
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 {
				seconds = min(s, 3)
			}
			timer := time.NewTimer(time.Duration(seconds) * time.Second)
			select {
			case <-timer.C:
				continue
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return fmt.Errorf("MLflow %s returned HTTP %d", route, resp.StatusCode)
		}
		if out != nil {
			err = json.NewDecoder(resp.Body).Decode(out)
		} else {
			_, err = io.Copy(io.Discard, resp.Body)
		}
		resp.Body.Close()
		return err
	}
}

var mlflowPass = regexp.MustCompile(`(^|/)pass([@^])([0-9]+)($|_)`)
var mlflowName = regexp.MustCompile(`^[\p{L}\p{N}_ /:.\-]+$`)
var mlflowSensitive = regexp.MustCompile(`(?i)(?:^|[._/-])(?:api[_-]?key|secret|passwd|password|authorization|bearer|credential|(?:auth|access|tracking|refresh)[_-]?token|token)(?:$|[._/-])`)

func TranslateMLflowKey(key string) (string, bool) {
	name := mlflowPass.ReplaceAllStringFunc(key, func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "pass@", "pass_at_"), "pass^", "pass_all_")
	})
	ok := mlflowName.MatchString(name) && utf8.RuneCountInString(name) <= 250 && path.Clean(name) == name && !strings.HasPrefix(name, "/") && !strings.HasPrefix(name, "../") && name != "." && name != ".."
	return name, ok
}
func mlflowSanitize(key string, value any) string {
	text := fmt.Sprint(value)
	if mlflowSensitive.MatchString(key) {
		return "[REDACTED]"
	}
	for _, p := range []string{"fw_", "fwml1.", "sk-", "ghp_", "ghs_", "github_pat_", "Bearer "} {
		if strings.HasPrefix(text, p) {
			return "[REDACTED]"
		}
	}
	chars := []rune(text)
	if len(chars) > 500 {
		return string(chars[:500])
	}
	return text
}
func mlflowKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func mlflowTags(tags map[string]string) []map[string]any {
	entries := make([]map[string]any, 0, len(tags))
	for _, k := range mlflowKeys(tags) {
		if _, ok := TranslateMLflowKey(k); ok {
			entries = append(entries, map[string]any{"key": k, "value": mlflowSanitize(k, tags[k])})
		}
	}
	return entries
}
func mlflowNumber(value any) (float64, bool) {
	v := reflect.ValueOf(value)
	var n float64
	switch v.Kind() {
	case reflect.Float32, reflect.Float64:
		n = v.Float()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n = float64(v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n = float64(v.Uint())
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}
func TranslateMLflowMetrics(values map[string]any) map[string]float64 {
	out := map[string]float64{}
	for _, key := range mlflowKeys(values) {
		if key == "step" || key == "session_id" {
			continue
		}
		number, ok := mlflowNumber(values[key])
		if !ok {
			continue
		}
		name, ok := TranslateMLflowKey(key)
		if _, exists := out[name]; ok && !exists {
			out[name] = number
		}
	}
	return out
}
func (c *MLflowIngestClient) Log(data map[string]any, step int) {
	values := TranslateMLflowMetrics(data)
	now := time.Now().UnixMilli()
	entries := make([]map[string]any, 0, len(values))
	for _, key := range mlflowKeys(values) {
		entries = append(entries, map[string]any{"key": key, "value": values[key], "step": step, "timestamp": now})
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enqueue("metrics", entries)
}
func (c *MLflowIngestClient) SetTags(tags map[string]string) {
	entries := mlflowTags(tags)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enqueue("tags", entries)
}

func (c *MLflowIngestClient) LogParams(params map[string]any) {
	if len(params) == 0 {
		return
	}
	flat := map[string]string{}
	// Go configurations often mix map[string]string and map[string]any. Flatten
	// every string-keyed map so nested secrets receive the same key-based redaction.
	visiting := make(map[uintptr]bool)
	var flatten func(string, reflect.Value)
	flatten = func(prefix string, m reflect.Value) {
		id := m.Pointer()
		if visiting[id] {
			return
		}
		visiting[id] = true
		defer delete(visiting, id)
		iter := m.MapRange()
		for iter.Next() {
			v := iter.Value().Interface()
			if v == nil {
				continue
			}
			key := prefix + iter.Key().String()
			if _, ok := TranslateMLflowKey(key); !ok {
				continue
			}
			child := reflect.ValueOf(v)
			if child.Kind() == reflect.Map && child.Type().Key().Kind() == reflect.String {
				flatten(key+".", child)
			} else {
				flat[key] = mlflowSanitize(key, v)
			}
		}
	}
	flatten("", reflect.ValueOf(params))
	c.mu.Lock()
	defer c.mu.Unlock()
	entries := make([]map[string]any, 0, len(flat))
	for _, key := range mlflowKeys(flat) {
		if !c.params[key] {
			entries = append(entries, map[string]any{"key": key, "value": flat[key]})
		}
	}
	if c.enqueue("params", entries) {
		for _, e := range entries {
			c.params[e["key"].(string)] = true
		}
	}
}
func (c *MLflowIngestClient) enqueue(kind string, entries []map[string]any) bool {
	if c.closed || c.err != nil || len(entries) == 0 {
		return false
	}
	select {
	case c.queue <- mlflowBatch{kind, entries}:
		return true
	default:
		return false
	}
}
func (c *MLflowIngestClient) send() {
	defer close(c.done)
	defer c.http.CloseIdleConnections()
	for batch := range c.queue {
		limit := 100
		if batch.kind == "metrics" {
			limit = 1000
		}
		for len(batch.entries) > 0 {
			n := min(limit, len(batch.entries))
			body := map[string]any{"run_id": c.RunID, "tags": []any{}, "params": []any{}, "metrics": []any{}}
			body[batch.kind] = batch.entries[:n]
			if err := c.post(context.Background(), "/api/2.0/mlflow/runs/log-batch", body, nil); err != nil {
				c.mu.Lock()
				c.err = err
				c.mu.Unlock()
				return
			}
			batch.entries = batch.entries[n:]
		}
	}
	err := c.post(context.Background(), "/api/2.0/mlflow/runs/update", map[string]any{"run_id": c.RunID, "status": c.status, "end_time": time.Now().UnixMilli()}, nil)
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
}

// Finish waits up to five seconds for queued writes. A later call can wait again.
func (c *MLflowIngestClient) Finish(ctx context.Context, status string) error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.status = mlflowFirst(status, "FINISHED")
		close(c.queue)
	}
	c.mu.Unlock()
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-c.done:
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.err
	case <-wait.Done():
		return wait.Err()
	}
}
