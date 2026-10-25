//go:build !linux && !darwin

package tito

import (
	"fmt"
	"os"
)

// Fail closed where the native no-follow/owner primitives are unavailable.
func privateTraceRoot(os.FileInfo) bool { return false }
func traceProcessGone(int) bool         { return false }
func traceFreeBytes(string) (int64, error) {
	return 0, fmt.Errorf("secure trace storage unavailable on this platform")
}
func openTraceAppend(string) (*os.File, error) {
	return nil, fmt.Errorf("secure trace storage unavailable on this platform")
}
func openCollectedTrace(string, string, string) (*os.File, error) {
	return nil, fmt.Errorf("secure collected trace reads unavailable on this platform")
}
