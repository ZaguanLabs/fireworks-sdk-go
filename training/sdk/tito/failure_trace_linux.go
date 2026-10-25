//go:build linux

package tito

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func privateTraceRoot(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0077 == 0 && ok && stat.Uid == uint32(os.Geteuid())
}
func traceProcessGone(pid int) bool { return syscall.Kill(pid, 0) == syscall.ESRCH }
func traceFreeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	return int64(stat.Bavail) * int64(stat.Bsize), err
}
func openTraceAppend(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func openCollectedTrace(source, root, rel string) (*os.File, error) {
	parent, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer func() { syscall.Close(parent) }()
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		next, err := syscall.Openat(parent, part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			return nil, err
		}
		syscall.Close(parent)
		parent = next
	}
	fd, err := syscall.Openat(parent, parts[len(parts)-1], syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), source)
	return file, nil
}
