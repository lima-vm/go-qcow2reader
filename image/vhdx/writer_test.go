package vhdx_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/convert"
	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/vhdx"
	"github.com/lima-vm/go-qcow2reader/test/qemuimg"
	"github.com/lima-vm/go-qcow2reader/test/qemuio"
)

const (
	KiB = int64(1) << 10
	MiB = int64(1) << 20
	GiB = int64(1) << 30
	TiB = int64(1) << 40

	// The offset of the first payload block for an image whose BAT fits in 1
	// MiB: 1 MiB header section, 1 MiB log, 1 MiB metadata, 1 MiB BAT.
	payloadOffset = 4 * MiB
)

// TestWriterConvert converts qcow2 images to VHDX with convert.Convert and
// verifies the result with qemu-img.
func TestWriterConvert(t *testing.T) {
	type testCase struct {
		name      string
		size      int64
		blockSize int64
		// Extents to write to the source image. Extents that are not allocated
		// are skipped.
		extents []image.Extent
		// Expected number of allocated payload blocks in the VHDX image.
		allocatedBlocks int64
	}
	testCases := []testCase{
		{
			name: "empty",
			size: 1 * GiB,
		},
		{
			name: "default block size",
			size: 64 * MiB, // 1 MiB blocks
			extents: []image.Extent{
				{Start: 1 * MiB, Length: 64 * KiB, Allocated: true},
				{Start: 20*MiB + 4*KiB, Length: 8 * KiB, Allocated: true},
				{Start: 64*MiB - 512, Length: 512, Allocated: true},
			},
			allocatedBlocks: 3,
		},
		{
			name: "zero extents",
			size: 64 * MiB, // 1 MiB blocks
			extents: []image.Extent{
				{Start: 1 * MiB, Length: 1 * MiB, Allocated: true, Zero: true},
				{Start: 20 * MiB, Length: 8 * MiB, Allocated: true, Zero: true},
				{Start: 40 * MiB, Length: 64 * KiB, Allocated: true},
			},
			allocatedBlocks: 1,
		},
		{
			// The chunk ratio for 1 MiB blocks is 4096, so a 5 GiB image has two
			// chunks and the BAT contains an interleaved sector bitmap entry.
			name:      "small blocks, multiple chunks",
			size:      5 * GiB,
			blockSize: 1 * MiB,
			extents: []image.Extent{
				{Start: 0, Length: 4 * KiB, Allocated: true},
				{Start: 3*MiB + 512, Length: 2 * MiB, Allocated: true},
				{Start: 4*GiB - 64*KiB, Length: 128 * KiB, Allocated: true},
				{Start: 4*GiB + 3*MiB, Length: 4 * KiB, Allocated: true},
				{Start: 5*GiB - 64*KiB, Length: 64 * KiB, Allocated: true},
			},
			allocatedBlocks: 1 + 3 + 2 + 1 + 1,
		},
		{
			name:      "partial last block",
			size:      5*GiB + 1*MiB,
			blockSize: 16 * MiB,
			extents: []image.Extent{
				{Start: 5*GiB + 512*KiB, Length: 64 * KiB, Allocated: true},
			},
			allocatedBlocks: 1,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src.qcow2")
			dst := filepath.Join(dir, "dst.vhdx")

			if err := qemuimg.Create(src, qemuimg.FormatQcow2, tc.size, "", ""); err != nil {
				t.Fatal(err)
			}
			for _, extent := range tc.extents {
				if !extent.Allocated {
					continue
				}
				var err error
				if extent.Zero {
					err = qemuio.Zero(src, qemuimg.FormatQcow2, extent.Start, extent.Length)
				} else {
					err = qemuio.Write(src, qemuimg.FormatQcow2, extent.Start, extent.Length, 0x55)
				}
				if err != nil {
					t.Fatal(err)
				}
			}

			w := convertToVHDX(t, src, dst, vhdx.WriterOptions{BlockSize: tc.blockSize})

			info, err := qemuimg.Info(dst, qemuimg.FormatVhdx)
			if err != nil {
				t.Fatal(err)
			}
			if info.Format != string(qemuimg.FormatVhdx) {
				t.Errorf("expected format %q, got %q", qemuimg.FormatVhdx, info.Format)
			}
			if info.VirtualSize != tc.size {
				t.Errorf("expected virtual size %d, got %d", tc.size, info.VirtualSize)
			}
			if info.ClusterSize != w.BlockSize() {
				t.Errorf("expected block size %d, got %d", w.BlockSize(), info.ClusterSize)
			}

			// Only the written blocks must be allocated.
			st, err := os.Stat(dst)
			if err != nil {
				t.Fatal(err)
			}
			expectedSize := payloadOffset + tc.allocatedBlocks*w.BlockSize()
			if st.Size() != expectedSize {
				t.Errorf("expected file size %d, got %d", expectedSize, st.Size())
			}

			if err := qemuimg.Compare(dst, qemuimg.FormatVhdx, src, qemuimg.FormatQcow2); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestWriterWriteAt writes to the Writer directly, exercising writes spanning
// multiple blocks, writes of zeros, and overwrites.
func TestWriterWriteAt(t *testing.T) {
	const (
		size      = 8 * MiB
		blockSize = 1 * MiB
	)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.vhdx")
	raw := filepath.Join(dir, "dst.raw")

	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck

	w, err := vhdx.NewWriter(f, size, vhdx.WriterOptions{BlockSize: blockSize})
	if err != nil {
		t.Fatal(err)
	}

	expected := make([]byte, size)
	write := func(off int64, data []byte) {
		t.Helper()
		n, err := w.WriteAt(data, off)
		if err != nil {
			t.Fatal(err)
		}
		if n != len(data) {
			t.Fatalf("expected %d bytes written, got %d", len(data), n)
		}
		copy(expected[off:], data)
	}
	pattern := func(n int64, b byte) []byte {
		return bytes.Repeat([]byte{b}, int(n))
	}

	// A write spanning 4 blocks, unaligned to the block size.
	write(512*KiB, pattern(3*MiB+4*KiB, 0xaa))
	// Zeros to an unallocated block do not allocate the block.
	write(5*MiB, pattern(1*MiB, 0))
	// Zeros within an allocated block are written.
	write(1*MiB+4*KiB, pattern(8*KiB, 0))
	// Overwrite.
	write(2*MiB-1*KiB, pattern(2*KiB, 0xbb))
	// The last sector.
	write(size-512, pattern(512, 0xcc))
	// An empty write is a no-op.
	write(6*MiB, nil)

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Blocks 0, 1, 2, 3, and 7 are allocated.
	st, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if expectedSize := payloadOffset + 5*blockSize; st.Size() != expectedSize {
		t.Errorf("expected file size %d, got %d", expectedSize, st.Size())
	}

	if err := qemuimg.Convert(dst, raw, qemuimg.FormatRaw, qemuimg.CompressionNone); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(expected) {
		t.Fatalf("expected %d bytes, got %d", len(expected), len(actual))
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("converted image differs from the written data at offset %d: expected %#x, got %#x", i, expected[i], actual[i])
		}
	}
}

func TestWriterClosed(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst.vhdx")
	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck

	w, err := vhdx.NewWriter(f, 1*GiB, vhdx.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("expected %v, got %v", os.ErrClosed, err)
	}
	if _, err := w.WriteAt([]byte{1}, 0); !errors.Is(err, os.ErrClosed) {
		t.Errorf("expected %v, got %v", os.ErrClosed, err)
	}
}

func TestWriterOutOfBounds(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst.vhdx")
	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck

	const size = 1 * GiB
	w, err := vhdx.NewWriter(f, size, vhdx.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close() //nolint:errcheck

	for _, tc := range []struct {
		off    int64
		length int
	}{
		{-1, 1},
		{size, 1},
		{size - 1, 2},
		{size + 1, 0},
	} {
		if _, err := w.WriteAt(make([]byte, tc.length), tc.off); err == nil {
			t.Errorf("expected error for write of %d bytes at %d", tc.length, tc.off)
		}
	}
	// Writes at the end of the disk are fine.
	if _, err := w.WriteAt(make([]byte, 1), size-1); err != nil {
		t.Error(err)
	}
	if _, err := w.WriteAt(nil, size); err != nil {
		t.Error(err)
	}
}

func TestWriterInvalidOptions(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst.vhdx")
	f, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck

	for _, tc := range []struct {
		name      string
		size      int64
		blockSize int64
	}{
		{name: "zero size", size: 0},
		{name: "negative size", size: -512},
		{name: "unaligned size", size: 1000},
		{name: "too large", size: 64*TiB + 512},
		{name: "block size too small", size: 1 * GiB, blockSize: 512 * KiB},
		{name: "block size too large", size: 1 * GiB, blockSize: 512 * MiB},
		{name: "block size not power of two", size: 1 * GiB, blockSize: 3 * MiB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := vhdx.NewWriter(f, tc.size, vhdx.WriterOptions{BlockSize: tc.blockSize}); err == nil {
				t.Errorf("expected error for size %d, block size %d", tc.size, tc.blockSize)
			}
		})
	}
}

func TestDefaultBlockSize(t *testing.T) {
	for _, tc := range []struct {
		size     int64
		expected int64
	}{
		{512, 1 * MiB},
		{1 * GiB, 1 * MiB},
		{1 * TiB, 1 * MiB},
		{1*TiB + 512, 2 * MiB},
		{2 * TiB, 2 * MiB},
		{2*TiB + 512, 4 * MiB},
		{64 * TiB, 64 * MiB},
	} {
		if actual := vhdx.DefaultBlockSize(tc.size); actual != tc.expected {
			t.Errorf("size %d: expected block size %d, got %d", tc.size, tc.expected, actual)
		}
	}
}

// convertToVHDX converts the image at src to a VHDX image at dst, and returns
// the closed Writer.
func convertToVHDX(t *testing.T, src, dst string, opts vhdx.WriterOptions) *vhdx.Writer {
	t.Helper()

	sf, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer sf.Close() //nolint:errcheck
	img, err := qcow2reader.Open(sf)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close() //nolint:errcheck

	df, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer df.Close() //nolint:errcheck

	w, err := vhdx.NewWriter(df, img.Size(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := convert.Convert(w, img, convert.Options{}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := df.Close(); err != nil {
		t.Fatal(err)
	}
	return w
}
