package qcow2reader

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
)

const (
	MiB = int64(1) << 20
	GiB = int64(1) << 30

	CompressionTypeNone = qcow2.CompressionType(255)
)

func BenchmarkRead(b *testing.B) {
	const size = 256 * MiB
	base := filepath.Join(b.TempDir(), "image")
	if err := createTestImage(base, size); err != nil {
		b.Fatal(err)
	}
	b.Run("qcow2", func(b *testing.B) {
		img := base + ".qocw2"
		if err := qemuImgConvert(base, img, qcow2.Type, CompressionTypeNone); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkRead(b, img)
		}
	})
	b.Run("qcow2 zlib", func(b *testing.B) {
		img := base + ".zlib.qcow2"
		if err := qemuImgConvert(base, img, qcow2.Type, qcow2.CompressionTypeZlib); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkRead(b, img)
		}
	})
	// TODO: qcow2 zstd (not supported yet)
}

func benchmarkRead(b *testing.B, filename string) {
	b.StartTimer()

	f, err := os.Open(filename)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	img, err := Open(f)
	if err != nil {
		b.Fatal(err)
	}
	defer img.Close()
	buf := make([]byte, 1*MiB)
	reader := io.NewSectionReader(img, 0, img.Size())
	n, err := io.CopyBuffer(io.Discard, reader, buf)

	b.StopTimer()

	if err != nil {
		b.Fatal(err)
	}
	if n != img.Size() {
		b.Fatalf("Expected %d bytes, read %d bytes", img.Size(), n)
	}
}

func resetBenchmark(b *testing.B, size int64) {
	b.StopTimer()
	b.ResetTimer()
	b.SetBytes(size)
	b.ReportAllocs()
}

// createTestImage creates a 50% allocated raw image with fake data that
// compresses like real image data.
func createTestImage(filename string, size int64) error {
	const chunkSize = 4 * MiB
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Truncate(size); err != nil {
		return err
	}
	reader := &Generator{}
	for offset := int64(0); offset < size; offset += 2 * chunkSize {
		_, err := file.Seek(offset, io.SeekStart)
		if err != nil {
			return err
		}
		chunk := io.LimitReader(reader, chunkSize)
		if n, err := io.Copy(file, chunk); err != nil {
			return err
		} else if n != chunkSize {
			return fmt.Errorf("expected %d bytes, wrote %d bytes", chunkSize, n)
		}
	}
	return file.Close()
}

// Generator generates fake data that compresses like a real image data (30%).
type Generator struct{}

func (g *Generator) Read(b []byte) (int, error) {
	for i := 0; i < len(b); i++ {
		b[i] = byte(i & 0xff)
	}
	rand.Shuffle(len(b)/8*5, func(i, j int) {
		b[i], b[j] = b[j], b[i]
	})
	return len(b), nil
}

func qemuImgConvert(src, dst string, dstFormat image.Type, compressionType qcow2.CompressionType) error {
	args := []string{"convert", "-O", string(dstFormat)}
	if compressionType != CompressionTypeNone {
		args = append(args, "-c", "-o", "compression_type="+compressionType.String())
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
