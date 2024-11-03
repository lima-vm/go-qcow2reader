package qemuio

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/lima-vm/go-qcow2reader/test/qemuimg"
)

// Write writes a number of bytes at a specified offset, allocating all clusters
// in specified range.
func Write(path string, format qemuimg.Format, off, len int64, pattern byte) error {
	// qemu-io -f qcow2 -c 'write -P pattern off len' disk.qcow2
	command := fmt.Sprintf("write -P %d %d %d", pattern, off, len)
	_, err := qemuIo([]string{"-f", string(format), "-c", command, path})
	return err
}

// Zero writes zeros at a specified offset. The behavior depens on qcow2
// version; In qcow2 v3, allocate zero clusters, marming entire cluster as zero.
// In qcow2 v2, if the cluster are unallocated and there is no backing file, do
// nothing. Otherwise allocate clusters and write actual zeros.
func Zero(path string, format qemuimg.Format, off, len int64) error {
	// qemu-io -f qcow2 -c 'write -z off len' disk.qcow2
	command := fmt.Sprintf("write -z %d %d", off, len)
	_, err := qemuIo([]string{"-f", string(format), "-c", command, path})
	return err
}

// Discard unmap number of bytes at specified offset. Allocated cluster are
// deaallocated and replaced with zero clusters.
func Discard(path string, format qemuimg.Format, off, len int64, unmap bool) error {
	// qemu-io -f qcow2 -c 'write -zu off len' disk.qcow2
	command := fmt.Sprintf("write -zu %d %d", off, len)
	_, err := qemuIo([]string{"-f", string(format), "-c", command, path})
	return err
}

func qemuIo(args []string) ([]byte, error) {
	cmd := exec.Command("qemu-io", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return out, fmt.Errorf("%w: stderr=%q", err, stderr.String())
		}
		return out, err
	}
	return out, nil
}
