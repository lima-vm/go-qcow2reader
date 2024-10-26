package qemuimg

import (
	"bytes"
	"errors"
	"os/exec"
)

type CompressionType string
type Format string

const (
	// Compression types.
	CompressionNone = CompressionType("")
	CompressionZlib = CompressionType("zlib")
	CompressionZstd = CompressionType("zstd")

	// Image formats.
	FormatQcow2 = Format("qcow2")
	FormatRaw   = Format("raw")
)

func Convert(src, dst string, dstFormat Format, compressionType CompressionType) error {
	args := []string{"convert", "-O", string(dstFormat)}
	if compressionType != CompressionNone {
		args = append(args, "-c", "-o", "compression_type="+string(compressionType))
	}
	args = append(args, src, dst)
	cmd := exec.Command("qemu-img", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Return qemu-img stderr instead of the unhelpful default error (exited
		// with status 1).
		if _, ok := err.(*exec.ExitError); ok {
			return errors.New(stderr.String())
		}
		return err
	}
	return nil
}
