//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package sdk

import "syscall"

const mlflowReadFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
