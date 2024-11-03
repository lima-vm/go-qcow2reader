package qemuimg

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
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
	_, err := qemuImg(args)
	return err
}

func Create(path string, format Format, size int64, backingFile string, backingFormat Format) error {
	args := []string{"create", "-f", string(format)}
	if backingFile != "" {
		args = append(args, "-b", backingFile, "-F", string(backingFormat))
	}
	args = append(args, path, strconv.FormatInt(size, 10))
	_, err := qemuImg(args)
	return err
}

func qemuImg(args []string) ([]byte, error) {
	cmd := exec.Command("qemu-img", args...)
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
