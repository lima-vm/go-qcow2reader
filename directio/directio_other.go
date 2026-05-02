// Must match the negation of the build tags in directio.go.
//go:build !linux && !freebsd && !netbsd && !dragonfly && !aix && !windows

package directio

import "syscall"

// Flag is the open flag for direct I/O. On platforms without O_DIRECT, we use
// O_DSYNC to emulate direct I/O, same as QEMU.
const Flag = syscall.O_DSYNC

// DefaultAlignment is 0 since O_DSYNC does not require aligned buffers.
const DefaultAlignment = 0
