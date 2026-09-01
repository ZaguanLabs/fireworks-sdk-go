package tito

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"
)

const (
	DebugFormat        = "fireworks-tito-debug-jsonl"
	DebugSchemaVersion = 1
)

var (
	safeComponentPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	secretKeyPattern     = regexp.MustCompile(`(?i)(authorization|cookie|api[-_]?key|bearer|access[-_]?token|secret)`)
	textKeyPattern       = regexp.MustCompile(`(?i)^(content|text|reasoning_content|arguments|instruction|prompt|problem_statement|completion_text|body|wire_request_body)$`)
	bearerValuePattern   = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	secretValuePattern   = regexp.MustCompile(`(?i)((?:api[-_]?key|token|secret)\s*[:=]\s*)[^\s,;]+`)
)

type DebugStorageFullError struct{ Message string }

func (e *DebugStorageFullError) Error() string     { return e.Message }
func (e *DebugStorageFullError) StorageFull() bool { return true }

type LocalDebugConfig struct {
	RootDir       string
	MaxLocalBytes int64
	MinFreeBytes  int64
	RunID         string
	WriterID      string
	RedactText    bool
	Metadata      map[string]any
}

func (c LocalDebugConfig) Validate() error {
	if !filepath.IsAbs(c.RootDir) {
		return errors.New("TITO debug root_dir must be absolute")
	}
	if c.MaxLocalBytes < 1 {
		return errors.New("max_local_bytes must be positive")
	}
	if c.MinFreeBytes < 0 {
		return errors.New("min_free_bytes must be non-negative")
	}
	if c.RunID != "" && !safeComponentPattern.MatchString(c.RunID) {
		return errors.New("run_id must contain only letters, digits, '.', '_' or '-'")
	}
	if c.WriterID != "" && !safeComponentPattern.MatchString(c.WriterID) {
		return errors.New("writer_id must contain only letters, digits, '.', '_' or '-'")
	}
	_, err := json.Marshal(c.Metadata)
	return err
}

type LocalDebugSink struct {
	Config          LocalDebugConfig
	RunID           string
	WriterID        string
	RunDir          string
	WriterDir       string
	TrajectoriesDir string
	ManifestPath    string
	TombstonesPath  string

	mu           sync.Mutex
	closed       bool
	bytesWritten int64
	sequences    map[string]int
}

type TITODebugStorageFullError = DebugStorageFullError
type TITOLocalDebugConfig = LocalDebugConfig
type TITOLocalDebugSink = LocalDebugSink

func NewLocalDebugSink(config LocalDebugConfig) (*LocalDebugSink, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	runID := config.RunID
	if runID == "" {
		runID = fmt.Sprintf("%d-%s", time.Now().Unix(), randomHex(6))
	}
	writerID := config.WriterID
	if writerID == "" {
		writerID = fmt.Sprintf("pid%d-%s", os.Getpid(), randomHex(6))
	}
	runDir := filepath.Join(config.RootDir, "run="+runID)
	writerDir := filepath.Join(runDir, "writer="+writerID)
	sink := &LocalDebugSink{Config: config, RunID: runID, WriterID: writerID, RunDir: runDir, WriterDir: writerDir, TrajectoriesDir: filepath.Join(writerDir, "trajectories"), ManifestPath: filepath.Join(writerDir, "manifest.json"), TombstonesPath: filepath.Join(writerDir, "tombstones.jsonl"), sequences: make(map[string]int)}
	if err := mkdirPrivate(config.RootDir); err != nil {
		return nil, err
	}
	if err := mkdirPrivate(runDir); err != nil {
		return nil, err
	}
	if err := os.Mkdir(writerDir, 0o700); err != nil {
		return nil, err
	}
	if err := mkdirPrivate(sink.TrajectoriesDir); err != nil {
		return nil, err
	}
	manifest := map[string]any{"format": DebugFormat, "schema_version": DebugSchemaVersion, "run_id": runID, "writer_id": writerID, "created_at": nowSeconds(), "redact_text": config.RedactText, "metadata": cloneAnyMap(config.Metadata)}
	encoded, err := debugEncode(manifest)
	if err != nil {
		return nil, err
	}
	if err = sink.reserve(int64(len(encoded))); err != nil {
		return nil, err
	}
	if err = os.WriteFile(sink.ManifestPath, encoded, 0o600); err != nil {
		return nil, err
	}
	sink.bytesWritten += int64(len(encoded))
	return sink, nil
}

func (s *LocalDebugSink) Record(event, trajectoryID string, payload map[string]any, arrays map[string][]any) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("TITO debug sink is closed")
	}
	sequence := s.sequences[trajectoryID]
	value := map[string]any{"event": event, "event_seq": sequence, "recorded_at": nowSeconds(), "trajectory_id": trajectoryID, "payload": s.redact(cloneAnyMap(payload), "")}
	if len(arrays) > 0 {
		value["arrays"] = s.redact(arrays, "")
	}
	written, err := s.append(filepath.Join(s.trajectoryDir(trajectoryID), "events.jsonl"), value)
	if err == nil {
		s.sequences[trajectoryID] = sequence + 1
	}
	return written, err
}

func (s *LocalDebugSink) CloseTrajectory(trajectoryID string, status TrajectoryStatus, payload map[string]any) (int, error) {
	value := cloneAnyMap(payload)
	value["status"] = status
	return s.Record("trajectory_terminal", trajectoryID, value, nil)
}

func (s *LocalDebugSink) RecordTombstoneEvent(event, trajectoryID string, payload map[string]any) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, nil
	}
	return s.append(s.TombstonesPath, map[string]any{"event": event, "recorded_at": nowSeconds(), "trajectory_id": trajectoryID, "payload": s.redact(cloneAnyMap(payload), "")})
}
func (s *LocalDebugSink) Close() { s.mu.Lock(); s.closed = true; s.mu.Unlock() }

func (s *LocalDebugSink) trajectoryDir(id string) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(s.TrajectoriesDir, hex.EncodeToString(sum[:]))
}
func (s *LocalDebugSink) append(path string, value map[string]any) (int, error) {
	encoded, err := debugEncode(value)
	if err != nil {
		return 0, err
	}
	if err = s.reserve(int64(len(encoded))); err != nil {
		return 0, err
	}
	if err = mkdirPrivate(filepath.Dir(path)); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	_, writeErr := file.Write(encoded)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil {
		return 0, writeErr
	}
	if syncErr != nil {
		return 0, syncErr
	}
	if closeErr != nil {
		return 0, closeErr
	}
	s.bytesWritten += int64(len(encoded))
	return len(encoded), nil
}
func (s *LocalDebugSink) reserve(size int64) error {
	if s.bytesWritten+size > s.Config.MaxLocalBytes {
		return &DebugStorageFullError{Message: "TITO debug byte budget exhausted"}
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.Config.RootDir, &stat); err != nil {
		return err
	}
	free := int64(stat.Bavail) * int64(stat.Bsize)
	if free-size < s.Config.MinFreeBytes {
		return &DebugStorageFullError{Message: "TITO debug free-space floor reached"}
	}
	return nil
}

func (s *LocalDebugSink) redact(value any, key string) any {
	if key != "" && secretKeyPattern.MatchString(key) {
		return "<redacted>"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for itemKey, item := range typed {
			out[itemKey] = s.redact(item, itemKey)
		}
		return out
	case map[string][]any:
		out := make(map[string]any, len(typed))
		for itemKey, item := range typed {
			out[itemKey] = s.redact(item, itemKey)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = s.redact(item, "")
		}
		return out
	case string:
		if s.Config.RedactText && key != "" && textKeyPattern.MatchString(key) {
			sum := sha256.Sum256([]byte(typed))
			return map[string]any{"redacted": true, "bytes": len([]byte(typed)), "sha256": hex.EncodeToString(sum[:])}
		}
		text := bearerValuePattern.ReplaceAllString(typed, "${1}<redacted>")
		return secretValuePattern.ReplaceAllString(text, "${1}<redacted>")
	default:
		return value
	}
}

func debugEncode(value map[string]any) ([]byte, error) {
	encoded, err := canonicalJSON(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
func mkdirPrivate(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

var _ EventObserver = (*LocalDebugSink)(nil)
