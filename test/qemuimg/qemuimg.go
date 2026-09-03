package qemuimg

import (
	"bytes"
	"encoding/json"
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
	FormatVhdx  = Format("vhdx")
)

// ImageInfo is a subset of the output of "qemu-img info --output=json".
type ImageInfo struct {
	Format      string `json:"format"`
	VirtualSize int64  `json:"virtual-size"`
	ClusterSize int64  `json:"cluster-size"`
}

// Info returns information about an image.
func Info(path string, format Format) (*ImageInfo, error) {
	out, err := qemuImg([]string{"info", "--output=json", "-f", string(format), path})
	if err != nil {
		return nil, err
	}
	var info ImageInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Compare compares the contents of two images, returning an error if they
// differ.
func Compare(a string, aFormat Format, b string, bFormat Format) error {
	out, err := qemuImg([]string{"compare", "-f", string(aFormat), "-F", string(bFormat), a, b})
	if err != nil {
		return fmt.Errorf("%w: stdout=%q", err, string(out))
	}
	return nil
}

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
