// Package qcow2reader_test keeps blackbox tests for qcow2reader.
package qcow2reader_test

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/convert"
	"github.com/lima-vm/go-qcow2reader/test/qemuimg"
)

const (
	MiB = int64(1) << 20
	GiB = int64(1) << 30
)

// Benchmark completely empty sparse image (0% utilization).  This is the best
// case when we don't have to read any cluster from storage.
func Benchmark0p(b *testing.B) {
	const size = 256 * MiB
	base := filepath.Join(b.TempDir(), "image")
	if err := createTestImage(base, size, 0.0); err != nil {
		b.Fatal(err)
	}
	b.Run("qcow2", func(b *testing.B) {
		img := base + ".qocw2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionNone); err != nil {
			b.Fatal(err)
		}
		b.Run("read", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkRead(b, img)
			}
		})
		b.Run("convert", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkConvert(b, img)
			}
		})
	})
	b.Run("qcow2 zlib", func(b *testing.B) {
		img := base + ".zlib.qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			b.Fatal(err)
		}
		b.Run("read", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkRead(b, img)
			}
		})
		b.Run("read", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkConvert(b, img)
			}
		})
	})
	// TODO: qcow2 zstd (not supported yet)
}

// Benchmark sparse image with 50% utilization matching lima default image.
func Benchmark50p(b *testing.B) {
	const size = 256 * MiB
	base := filepath.Join(b.TempDir(), "image")
	if err := createTestImage(base, size, 0.5); err != nil {
		b.Fatal(err)
	}
	b.Run("qcow2", func(b *testing.B) {
		img := base + ".qocw2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionNone); err != nil {
			b.Fatal(err)
		}
		b.Run("read", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkRead(b, img)
			}
		})
		b.Run("convert", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkConvert(b, img)
			}
		})
	})
	b.Run("qcow2 zlib", func(b *testing.B) {
		img := base + ".zlib.qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			b.Fatal(err)
		}
		b.Run("read", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkRead(b, img)
			}
		})
		b.Run("convert", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkConvert(b, img)
			}
		})
	})
	// TODO: qcow2 zstd (not supported yet)
}

// Benchmark fully allocated image. This is the worst case for both uncompressed
// and compressed image when we must read all clusters from storage.
func Benchmark100p(b *testing.B) {
	const size = 256 * MiB
	base := filepath.Join(b.TempDir(), "image")
	if err := createTestImage(base, size, 1.0); err != nil {
		b.Fatal(err)
	}
	b.Run("qcow2", func(b *testing.B) {
		img := base + ".qocw2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionNone); err != nil {
			b.Fatal(err)
		}
		b.Run("read", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkRead(b, img)
			}
		})
		b.Run("convert", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkConvert(b, img)
			}
		})
	})
	b.Run("qcow2 zlib", func(b *testing.B) {
		img := base + ".zlib.qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			b.Fatal(err)
		}
		b.Run("read", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkRead(b, img)
			}
		})
		b.Run("convert", func(b *testing.B) {
			resetBenchmark(b, size)
			for i := 0; i < b.N; i++ {
				benchmarkConvert(b, img)
			}
		})
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
	img, err := qcow2reader.Open(f)
	if err != nil {
		b.Fatal(err)
	}
	defer img.Close()
	buf := make([]byte, 1*MiB)
	reader := io.NewSectionReader(img, 0, img.Size())
	n, err := io.CopyBuffer(Discard, reader, buf)

	b.StopTimer()

	if err != nil {
		b.Fatal(err)
	}
	if n != img.Size() {
		b.Fatalf("Expected %d bytes, read %d bytes", img.Size(), n)
	}
}

func benchmarkConvert(b *testing.B, filename string) {
	b.StartTimer()

	f, err := os.Open(filename)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	img, err := qcow2reader.Open(f)
	if err != nil {
		b.Fatal(err)
	}
	defer img.Close()
	dst, err := os.Create(filename + ".out")
	if err != nil {
		b.Fatal(err)
	}
	defer dst.Close()
	c, err := convert.New(convert.Options{})
	if err != nil {
		b.Fatal(err)
	}
	err = c.Convert(dst, img, img.Size())
	if err != nil {
		b.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		b.Fatal(err)
	}

	b.StopTimer()
}

// We cannot use io.Discard since it implements ReadFrom using small buffers
// size (8192), confusing our test results. Reads smaller than cluster size (64
// KiB) are extremely inefficient with compressed clusters.
type discard struct{}

func (discard) Write(p []byte) (int, error) {
	return len(p), nil
}

var Discard = discard{}

func resetBenchmark(b *testing.B, size int64) {
	b.StopTimer()
	b.ResetTimer()
	b.SetBytes(size)
	b.ReportAllocs()
}

// createTestImage creates raw image with fake data that compresses like real
// image data. Utilization deterimines the amount of data to allocate (0.0--1.0).
func createTestImage(filename string, size int64, utilization float64) error {
	if utilization < 0 || utilization > 1 {
		return fmt.Errorf("utilization out of range (0.0-1.0): %f", utilization)
	}

	const chunkSize = 8 * MiB
	dataSize := int64(float64(chunkSize) * utilization)

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Truncate(size); err != nil {
		return err
	}
	if dataSize > 0 {
		reader := &Generator{}
		for offset := int64(0); offset < size; offset += chunkSize {
			_, err := file.Seek(offset, io.SeekStart)
			if err != nil {
				return err
			}
			chunk := io.LimitReader(reader, dataSize)
			if n, err := io.Copy(file, chunk); err != nil {
				return err
			} else if n != dataSize {
				return fmt.Errorf("expected %d bytes, wrote %d bytes", dataSize, n)
			}
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
