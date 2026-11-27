package tito

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// FailureTraceConfig bounds private local evidence, independently of debug storage.
type FailureTraceConfig struct {
	RootDir            string
	MaxLocalBytes      int64
	MaxTrajectoryBytes int64
	MaxFailedTraces    int
	MaxActiveTraces    int
	MinFreeBytes       int64
	Enabled            bool
	StaleWriterAge     time.Duration
}
type TITOFailureTraceConfig = FailureTraceConfig

func DefaultFailureTraceConfig() FailureTraceConfig {
	return FailureTraceConfig{RootDir: filepath.Join(os.TempDir(), "fireworks-tito-traces"), MaxLocalBytes: (5000 + 128) * (256 << 20), MaxTrajectoryBytes: 256 << 20, MaxFailedTraces: 5000, MaxActiveTraces: 128, MinFreeBytes: 64 << 20, Enabled: true, StaleWriterAge: 7 * 24 * time.Hour}
}
func (c FailureTraceConfig) Validate() error {
	if !filepath.IsAbs(c.RootDir) || c.MaxLocalBytes < 1 || c.MaxTrajectoryBytes < 1 || c.MaxTrajectoryBytes > c.MaxLocalBytes || c.MaxFailedTraces < 1 || c.MaxActiveTraces < 1 || c.MinFreeBytes < 0 || c.StaleWriterAge <= 0 {
		return fmt.Errorf("invalid TITO failure trace limits")
	}
	return nil
}

type failureTrace struct {
	path         string
	size         int64
	sequence     int
	failed       bool
	truncated    bool
	storageError string
}
type failureTraceRecorder struct {
	config       FailureTraceConfig
	mu           sync.Mutex
	writer       string
	traces       map[string]*failureTrace
	failed       []string
	bytes        int64
	storageError string
}

func newFailureTraceRecorder(c FailureTraceConfig) *failureTraceRecorder {
	return &failureTraceRecorder{config: c, traces: map[string]*failureTrace{}}
}

func (r *failureTraceRecorder) prepare() error {
	if r.writer != "" {
		return nil
	}
	if err := os.MkdirAll(r.config.RootDir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(r.config.RootDir)
	if err != nil {
		return err
	}
	if !privateTraceRoot(info) {
		return fmt.Errorf("failure trace root must be private and owned by this user")
	}
	r.sweepStaleWriters()
	r.writer, err = os.MkdirTemp(r.config.RootDir, "writer-")
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	owner, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "hostname": host, "created": nowSeconds()})
	if err := os.WriteFile(filepath.Join(r.writer, "owner.json"), owner, 0600); err != nil {
		return err
	}
	return nil
}

// Only files inside a marked, dead, same-host writer are eligible for reclamation.
func (r *failureTraceRecorder) sweepStaleWriters() {
	entries, _ := os.ReadDir(r.config.RootDir)
	host, _ := os.Hostname()
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "writer-") || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(r.config.RootDir, entry.Name())
		data, err := readCollectedTrace(filepath.Join(dir, "owner.json"), r.config.RootDir, 4096)
		if err != nil {
			continue
		}
		var owner struct {
			PID     int     `json:"pid"`
			Host    string  `json:"hostname"`
			Created float64 `json:"created"`
		}
		if json.Unmarshal(data, &owner) != nil || owner.PID <= 0 || owner.Host != host || nowSeconds()-owner.Created < r.config.StaleWriterAge.Seconds() {
			continue
		}
		if !traceProcessGone(owner.PID) {
			continue
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		valid := true
		for _, file := range files {
			if file.IsDir() || file.Type()&os.ModeSymlink != 0 || (file.Name() != "owner.json" && filepath.Ext(file.Name()) != ".jsonl") {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		for _, file := range files {
			_ = os.Remove(filepath.Join(dir, file.Name()))
		}
		_ = os.Remove(dir)
	}
}

func (r *failureTraceRecorder) remove(id string) {
	trace := r.traces[id]
	if trace == nil {
		return
	}
	if err := os.Remove(trace.path); err != nil && !os.IsNotExist(err) {
		return
	}
	r.bytes -= trace.size
	delete(r.traces, id)
	for i, key := range r.failed {
		if key == id {
			r.failed = append(r.failed[:i], r.failed[i+1:]...)
			break
		}
	}
}

func (r *failureTraceRecorder) record(id, event string, payload any) {
	if r == nil || !r.config.Enabled {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.prepare(); err != nil {
		r.storageError = fmt.Sprintf("%T", err)
		return
	}
	trace := r.traces[id]
	if trace == nil {
		if len(r.traces)-len(r.failed) >= r.config.MaxActiveTraces {
			r.storageError = "active_trace_limit"
			return
		}
		hash := sha256.Sum256([]byte(id))
		trace = &failureTrace{path: filepath.Join(r.writer, hex.EncodeToString(hash[:])+".jsonl")}
		r.traces[id] = trace
	}
	if trace.truncated {
		return
	}
	trace.sequence++
	data, err := json.Marshal(map[string]any{"event": event, "event_seq": trace.sequence, "recorded_at": nowSeconds(), "trajectory_id": id, "payload": Sanitize(payload, false)})
	if err != nil {
		trace.storageError = fmt.Sprintf("%T", err)
		return
	}
	data = append(data, '\n')
	if trace.size+int64(len(data)) > r.config.MaxTrajectoryBytes {
		trace.truncated = true
		return
	}
	for r.bytes+int64(len(data)) > r.config.MaxLocalBytes && len(r.failed) > 0 {
		victim := r.failed[0]
		if victim == id {
			break
		}
		before := len(r.failed)
		r.remove(victim)
		if len(r.failed) == before {
			break
		}
	}
	freeBytes, freeErr := traceFreeBytes(r.config.RootDir)
	if r.bytes+int64(len(data)) > r.config.MaxLocalBytes || freeErr != nil || freeBytes-int64(len(data)) < r.config.MinFreeBytes {
		trace.storageError = "local_storage_limit"
		return
	}
	file, err := openTraceAppend(trace.path)
	if err != nil {
		trace.storageError = fmt.Sprintf("%T", err)
		return
	}

	n, writeErr := file.Write(data)
	closeErr := file.Close()
	trace.size += int64(n)
	r.bytes += int64(n)
	if writeErr != nil || closeErr != nil {
		trace.storageError = "trace_write_failed"
	}
}

func (r *failureTraceRecorder) evidence(id string) map[string]any {
	result := map[string]any{}
	if r == nil {
		return result
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if trace := r.traces[id]; trace != nil {
		result["trace_path"] = trace.path
		result["trace_truncated"] = trace.truncated
		if trace.storageError != "" {
			result["trace_storage_error"] = trace.storageError
		}
	}
	if r.storageError != "" {
		result["trace_storage_error"] = r.storageError
	}
	return result
}
func (r *failureTraceRecorder) closeTrajectory(id string, failed bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	trace := r.traces[id]
	if trace == nil {
		return
	}
	if !failed {
		r.remove(id)
		return
	}
	if !trace.failed {
		trace.failed = true
		r.failed = append(r.failed, id)
	}
	for len(r.failed) > r.config.MaxFailedTraces {
		before := len(r.failed)
		r.remove(r.failed[0])
		if before == len(r.failed) {
			break
		}
	}
}

// Sanitize removes credentials without changing sampled text unless requested.
func Sanitize(value any, redactText bool) any {
	var normalized any
	data, err := json.Marshal(value)
	if err == nil && json.Unmarshal(data, &normalized) == nil {
		value = normalized
	}
	return (&LocalDebugSink{Config: LocalDebugConfig{RedactText: redactText}}).redact(value, "")
}

func safeParserTag(value string) string {
	if regexp.MustCompile(`^[A-Za-z0-9_.:@-]{1,128}$`).MatchString(value) {
		return value
	}
	return "unclassified"
}

// readCollectedTrace rejects links in every component below the trusted root.
func readCollectedTrace(source, root string, limit int64) ([]byte, error) {
	if root == "" {
		root = filepath.Dir(source)
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, source)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("collected trace is outside trusted root")
	}
	file, err := openCollectedTrace(source, root, rel)
	if err != nil {
		return nil, err
	}

	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("collected trace must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("collected trace exceeds limit")
	}
	return data, err
}

type FailureTraceDumpOptions struct {
	Config          *FailureTraceConfig
	SourceTrace     string
	SourceTraceRoot string
}

var defaultFailureRecorder = newFailureTraceRecorder(DefaultFailureTraceConfig())

func DumpTITOFailureTrace(artifact *TrajectoryArtifact, cause error, code string, options ...FailureTraceDumpOptions) string {
	opts := FailureTraceDumpOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	recorder := defaultFailureRecorder
	if opts.Config != nil {
		if opts.Config.Validate() != nil {
			return ""
		}
		recorder = newFailureTraceRecorder(*opts.Config)
	}
	return dumpFailureTrace(recorder, artifact, cause, code, opts)
}

// DumpFailureTrace shares this sidecar's private storage and retention budget.
func (s *Sidecar) DumpFailureTrace(artifact *TrajectoryArtifact, cause error, code string, options ...FailureTraceDumpOptions) string {
	opts := FailureTraceDumpOptions{}
	if len(options) > 0 {
		opts = options[0]
	}
	return dumpFailureTrace(s.traces, artifact, cause, code, opts)
}
func dumpFailureTrace(recorder *failureTraceRecorder, artifact *TrajectoryArtifact, cause error, code string, opts FailureTraceDumpOptions) string {
	if !recorder.config.Enabled {
		return ""
	}
	id := "artifact-" + randomHex(16)
	payload := map[string]any{"code": safeParserTag(code)}
	if cause != nil {
		payload["exception_chain"] = Sanitize(cause.Error(), false)
	}
	if artifact != nil {
		data, err := artifact.Pack()
		if err != nil {
			return ""
		}
		payload["artifact_encoding"] = "base64"
		payload["artifact_base64"] = base64.StdEncoding.EncodeToString(data)
	}
	if opts.SourceTrace != "" {
		data, err := readCollectedTrace(opts.SourceTrace, opts.SourceTraceRoot, recorder.config.MaxTrajectoryBytes)
		if err != nil {
			payload["collected_trace_error"] = fmt.Sprintf("%T", err)
		} else {
			payload["collected_trace_jsonl"] = string(data)
		}
	}
	if artifact == nil && opts.SourceTrace == "" {
		return ""
	}
	recorder.record(id, "call_terminal", payload)
	recorder.closeTrajectory(id, true)
	path, _ := recorder.evidence(id)["trace_path"].(string)
	return path
}
