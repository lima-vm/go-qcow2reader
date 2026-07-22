// Platforms that define syscall.O_DIRECT, verified by checking for O_DIRECT in
// zerrors_*_*.go in the Go source ($GOROOT/src/syscall).
// When changing these tags, update directio_other.go to match.
//go:build linux || freebsd || netbsd || dragonfly || aix

package directio

import "syscall"

// Flag is the open flag for direct I/O.
const Flag = syscall.O_DIRECT

// DefaultAlignment is a safe default buffer alignment for direct I/O. The
// actual requirement may be lower depending on the file system or block device.
const DefaultAlignment = 4096
