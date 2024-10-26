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
	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/test/qemuimg"
)

const (
	MiB = int64(1) << 20
	GiB = int64(1) << 30
)

func TestExtentsUnallocated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image")
	if err := qemuimg.Create(path, qemuimg.FormatQcow2, 4*GiB, "", ""); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := qcow2reader.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()

	t.Run("entire image", func(t *testing.T) {
		actual, err := img.Extent(0, img.Size())
		if err != nil {
			t.Fatal(err)
		}
		expected := image.Extent{Start: 0, Length: img.Size(), Zero: true}
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
	t.Run("same result", func(t *testing.T) {
		r1, err := img.Extent(0, img.Size())
		if err != nil {
			t.Fatal(err)
		}
		r2, err := img.Extent(0, img.Size())
		if err != nil {
			t.Fatal(err)
		}
		if r1 != r2 {
			t.Fatalf("expected %+v, got %+v", r1, r2)
		}
	})
	t.Run("all segments", func(t *testing.T) {
		for i := int64(0); i < img.Size(); i += 32 * MiB {
			segment, err := img.Extent(i, 32*MiB)
			if err != nil {
				t.Fatal(err)
			}
			expected := image.Extent{Start: i, Length: 32 * MiB, Zero: true}
			if segment != expected {
				t.Fatalf("expected %+v, got %+v", expected, segment)
			}
		}
	})
	t.Run("start unaligned", func(t *testing.T) {
		start := 32*MiB + 42
		length := 32 * MiB
		actual, err := img.Extent(start, length)
		if err != nil {
			t.Fatal(err)
		}
		expected := image.Extent{Start: start, Length: length, Zero: true}
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
	t.Run("length unaligned", func(t *testing.T) {
		start := 32 * MiB
		length := 32*MiB - 42
		actual, err := img.Extent(start, length)
		if err != nil {
			t.Fatal(err)
		}
		expected := image.Extent{Start: start, Length: length, Zero: true}
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
	t.Run("start and length unaligned", func(t *testing.T) {
		start := 32*MiB + 42
		length := 32*MiB - 42
		actual, err := img.Extent(start, length)
		if err != nil {
			t.Fatal(err)
		}
		expected := image.Extent{Start: start, Length: length, Zero: true}
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
	t.Run("length after end of image", func(t *testing.T) {
		start := img.Size() - 31*MiB
		actual, err := img.Extent(start, 32*MiB)
		if err == nil {
			t.Fatal("out of bounds request did not fail")
		}
		var expected image.Extent
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
	t.Run("start after end of image", func(t *testing.T) {
		start := img.Size() + 1*MiB
		actual, err := img.Extent(start, 32*MiB)
		if err == nil {
			t.Fatal("out of bounds request did not fail")
		}
		var expected image.Extent
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
}

func TestExtentsRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.img")
	size := 4 * GiB
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	img, err := qcow2reader.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()

	t.Run("entire image", func(t *testing.T) {
		actual, err := img.Extent(0, img.Size())
		if err != nil {
			t.Fatal(err)
		}
		// Currently we always report raw images as fully allocated.
		expected := image.Extent{Start: 0, Length: img.Size(), Allocated: true}
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
	t.Run("length after end of image", func(t *testing.T) {
		start := img.Size() - 31*MiB
		actual, err := img.Extent(start, 32*MiB)
		if err == nil {
			t.Fatal("out of bounds request did not fail")
		}
		var expected image.Extent
		if actual != expected {
			t.Fatalf("expected %+v, got %+v", expected, actual)
		}
	})
}

func BenchmarkExtentsUnallocated(b *testing.B) {
	path := filepath.Join(b.TempDir(), "image")
	if err := qemuimg.Create(path, qemuimg.FormatQcow2, 100*GiB, "", ""); err != nil {
		b.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	img, err := qcow2reader.Open(f)
	if err != nil {
		b.Fatal(err)
	}
	expected := image.Extent{Start: 0, Length: img.Size(), Zero: true}
	resetBenchmark(b, img.Size())
	for i := 0; i < b.N; i++ {
		b.StartTimer()
		actual, err := img.Extent(0, img.Size())
		b.StopTimer()
		if err != nil {
			b.Fatal(err)
		}
		if actual != expected {
			b.Fatalf("expected %+v, got %+v", expected, actual)
		}
	}
}

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
