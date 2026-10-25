//go:build darwin

package tito

import (
	"fmt"
	"os"
	"syscall"
)

func privateTraceRoot(os.FileInfo) bool { return false }
func traceProcessGone(int) bool         { return false }
func traceFreeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	return int64(stat.Bavail) * int64(stat.Bsize), err
}
func openTraceAppend(string) (*os.File, error) {
	return nil, fmt.Errorf("secure trace storage unavailable on this platform")
}
func openCollectedTrace(string, string, string) (*os.File, error) {
	return nil, fmt.Errorf("secure collected trace reads unavailable on this platform")
}
