package tito

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type brokenRenderer struct{ fakeRenderer }

func (brokenRenderer) ParseAssistant(ChatRequest, []int, string, string) (ParsedAssistant, error) {
	return ParsedAssistant{}, errors.New("private parser internals")
}
func privateTraceConfig(t *testing.T) FailureTraceConfig {
	t.Helper()
	c := DefaultFailureTraceConfig()
	c.RootDir = t.TempDir()
	if err := os.Chmod(c.RootDir, 0700); err != nil {
		t.Fatal(err)
	}
	c.MinFreeBytes = 0
	return c
}
func Test1230ParserFailurePoisonsTrajectory(t *testing.T) {
	config := privateTraceConfig(t)
	s, err := NewSidecar(&fakeSampler{}, brokenRenderer{}, SidecarOptions{MaxContextTokens: 32, MaxOutputTokens: 4, FailureTraceConfig: &config})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	endpoint, err := s.CreateTrajectory("broken", "affinity", nil)
	if err != nil {
		t.Fatal(err)
	}
	trajectory, _ := s.trajectoryFor(endpoint.TrajectoryID)
	request := ChatRequest{Messages: []map[string]any{{"role": "user", "content": "hello"}}}
	_, err = s.complete(context.Background(), trajectory, request, "")
	var typed *Error
	if !errors.As(err, &typed) || !typed.DiscardTrajectory || typed.Code != "tito_sdk_parser_internal_error" || strings.Contains(typed.Message, "private") {
		t.Fatal(err)
	}
	artifact, err := s.FinishTrajectory(endpoint.TrajectoryID)
	if err != nil || artifact.Status != TrajectoryStatusFailed {
		t.Fatal(artifact, err)
	}
	if runtime.GOOS != "linux" {
		return
	}
	path, _ := s.traces.evidence(endpoint.TrajectoryID)["trace_path"].(string)
	if path == "" {
		t.Fatal("failure trace missing")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
func Test1230FailureTraceBoundsAndSecureRead(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("secure trace primitives currently require Linux")
	}
	config := privateTraceConfig(t)
	config.MaxFailedTraces = 1
	config.MaxTrajectoryBytes = 1024
	config.MaxLocalBytes = 4096
	recorder := newFailureTraceRecorder(config)
	recorder.record("one", "event", map[string]any{"authorization": "Bearer secret", "content": "hello"})
	first := recorder.evidence("one")["trace_path"].(string)
	data, err := os.ReadFile(first)
	if err != nil || strings.Contains(string(data), "secret") {
		t.Fatal(string(data), err)
	}
	if info, err := os.Stat(first); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	recorder.closeTrajectory("one", true)
	recorder.record("two", "event", map[string]any{"content": "hello"})
	recorder.closeTrajectory("two", true)
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("old failed trace not pruned", err)
	}
	recorder.record("large", "event", strings.Repeat("x", 2000))
	if recorder.evidence("large")["trace_truncated"] != true {
		t.Fatal("oversized trace unbounded")
	}
	source := filepath.Join(config.RootDir, "source.jsonl")
	if err := os.WriteFile(source, []byte("evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := readCollectedTrace(source, config.RootDir, 32); err != nil || string(data) != "evidence" {
		t.Fatal(string(data), err)
	}
	link := filepath.Join(config.RootDir, "link.jsonl")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readCollectedTrace(link, config.RootDir, 32); err == nil {
		t.Fatal("followed collected trace symlink")
	}
	if _, err := readCollectedTrace(source, config.RootDir, 2); err == nil {
		t.Fatal("oversized collected trace accepted")
	}
}
