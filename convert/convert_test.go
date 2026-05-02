package convert

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/test/qemuimg"
	"github.com/lima-vm/go-qcow2reader/test/testimage"
	. "github.com/lima-vm/go-qcow2reader/test/units" //nolint:staticcheck
)

func TestValidateAlignment(t *testing.T) {
	for _, alignment := range []int{512, 4096} {
		t.Run(fmt.Sprintf("%d", alignment), func(t *testing.T) {
			opts := Options{Alignment: alignment}
			if err := opts.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateAlignmentInvalid(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "alignment not power of 2",
			opts: Options{Alignment: 3000},
		},
		{
			name: "buffer size not aligned",
			opts: Options{Alignment: 4096, BufferSize: 5000, SegmentSize: 5000},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.Validate(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAllocateBuffer(t *testing.T) {
	opts := Options{}
	if err := opts.Validate(); err != nil {
		t.Fatal(err)
	}
	buf := allocateBuffer(&opts)
	if len(buf) != opts.BufferSize {
		t.Fatalf("expected len %d, got %d", opts.BufferSize, len(buf))
	}
}

func TestAllocateBufferAligned(t *testing.T) {
	for _, alignment := range []int{512, 4096} {
		t.Run(fmt.Sprintf("%d", alignment), func(t *testing.T) {
			opts := Options{
				Alignment: alignment,
			}
			if err := opts.Validate(); err != nil {
				t.Fatal(err)
			}
			buf := allocateBuffer(&opts)
			if len(buf) != opts.BufferSize {
				t.Fatalf("expected len %d, got %d", opts.BufferSize, len(buf))
			}
			addr := uintptr(unsafe.Pointer(&buf[0]))
			if addr%uintptr(alignment) != 0 {
				t.Fatalf("buffer address %x not aligned to %d", addr, alignment)
			}
		})
	}
}

// Benchmark completely empty sparse image (0% utilization). This is the best
// case when we don't have to read any cluster from storage.
func BenchmarkConvert0p(b *testing.B) {
	const size = 256 * MiB
	base := filepath.Join(tempDir(b), "image")
	if err := testimage.Create(base, size, 0.0); err != nil {
		b.Fatal(err)
	}
	b.Run("qcow2", func(b *testing.B) {
		img := base + ".qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionNone); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkConvert(b, img, Options{})
		}
	})
	b.Run("qcow2 zlib", func(b *testing.B) {
		img := base + ".zlib.qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkConvert(b, img, Options{})
		}
	})
}

// Benchmark sparse image with 50% utilization matching lima default image.
func BenchmarkConvert50p(b *testing.B) {
	const size = 256 * MiB
	base := filepath.Join(tempDir(b), "image")
	if err := testimage.Create(base, size, 0.5); err != nil {
		b.Fatal(err)
	}
	b.Run("qcow2", func(b *testing.B) {
		img := base + ".qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionNone); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkConvert(b, img, Options{})
		}
	})
	b.Run("qcow2 zlib", func(b *testing.B) {
		img := base + ".zlib.qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkConvert(b, img, Options{})
		}
	})
}

// Benchmark fully allocated image. This is the worst case for both uncompressed
// and compressed image when we must read all clusters from storage.
func BenchmarkConvert100p(b *testing.B) {
	const size = 256 * MiB
	base := filepath.Join(tempDir(b), "image")
	if err := testimage.Create(base, size, 1.0); err != nil {
		b.Fatal(err)
	}
	b.Run("qcow2", func(b *testing.B) {
		img := base + ".qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionNone); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkConvert(b, img, Options{})
		}
	})
	b.Run("qcow2 zlib", func(b *testing.B) {
		img := base + ".zlib.qcow2"
		if err := qemuimg.Convert(base, img, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			b.Fatal(err)
		}
		resetBenchmark(b, size)
		for i := 0; i < b.N; i++ {
			benchmarkConvert(b, img, Options{})
		}
	})
}

func benchmarkConvert(b *testing.B, filename string, opts Options) {
	b.StartTimer()

	f, err := os.Open(filename)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	img, err := qcow2reader.Open(f)
	if err != nil {
		b.Fatal(err)
	}
	defer img.Close() //nolint:errcheck
	dst, err := os.Create(filename + ".out")
	if err != nil {
		b.Fatal(err)
	}
	defer dst.Close() //nolint:errcheck
	if err := Convert(dst, img, opts); err != nil {
		b.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		b.Fatal(err)
	}

	b.StopTimer()
}

func resetBenchmark(b *testing.B, size int64) {
	b.StopTimer()
	b.ResetTimer()
	b.SetBytes(size)
	b.ReportAllocs()
}

// tempDir creates a temporary directory suitable for direct I/O testing. On
// Linux, /tmp is typically tmpfs which does not support O_DIRECT, so we use
// /var/tmp instead. On other systems the standard temp directory should work.
func tempDir(b *testing.B) string {
	if runtime.GOOS != "linux" {
		return b.TempDir()
	}
	dir, err := os.MkdirTemp("/var/tmp", "go-qcow2reader-"+b.Name()+"-")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
