// Package qcow2reader_test keeps blackbox tests for qcow2reader.
package qcow2reader_test

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/convert"
	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/test/qemuimg"
	"github.com/lima-vm/go-qcow2reader/test/qemuio"
)

const (
	KiB         = int64(1) << 10
	MiB         = int64(1) << 20
	GiB         = int64(1) << 30
	clusterSize = 64 * KiB
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

func TestExtentsSome(t *testing.T) {
	extents := []image.Extent{
		{Start: 0 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 1 * clusterSize, Length: 1 * clusterSize, Zero: true},
		{Start: 2 * clusterSize, Length: 2 * clusterSize, Allocated: true},
		{Start: 4 * clusterSize, Length: 96 * clusterSize, Zero: true},
		{Start: 100 * clusterSize, Length: 8 * clusterSize, Allocated: true},
		{Start: 108 * clusterSize, Length: 892 * clusterSize, Zero: true},
		{Start: 1000 * clusterSize, Length: 16 * clusterSize, Allocated: true},
		{Start: 1016 * clusterSize, Length: 8984 * clusterSize, Zero: true},
	}
	qcow2 := filepath.Join(t.TempDir(), "image")
	if err := createTestImageWithExtents(qcow2, qemuimg.FormatQcow2, extents, "", ""); err != nil {
		t.Fatal(err)
	}
	t.Run("qcow2", func(t *testing.T) {
		actual, err := listExtents(qcow2)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(extents, actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
	t.Run("qcow2 zlib", func(t *testing.T) {
		qcow2Zlib := qcow2 + ".zlib"
		if err := qemuimg.Convert(qcow2, qcow2Zlib, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			t.Fatal(err)
		}
		actual, err := listExtents(qcow2Zlib)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(compressed(extents), actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
}

func TestExtentsPartial(t *testing.T) {
	// Writing part of of a cluster allocates entire cluster in the qcow2 image.
	extents := []image.Extent{
		{Start: 0 * clusterSize, Length: 1, Allocated: true},
		{Start: 1 * clusterSize, Length: 98 * clusterSize, Zero: true},
		{Start: 100*clusterSize - 1, Length: 1, Allocated: true},
	}

	// Listing extents works in cluster granularity.
	full := []image.Extent{
		{Start: 0 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 1 * clusterSize, Length: 98 * clusterSize, Zero: true},
		{Start: 99 * clusterSize, Length: 1 * clusterSize, Allocated: true},
	}

	qcow2 := filepath.Join(t.TempDir(), "image")
	if err := createTestImageWithExtents(qcow2, qemuimg.FormatQcow2, extents, "", ""); err != nil {
		t.Fatal(err)
	}
	t.Run("qcow2", func(t *testing.T) {
		actual, err := listExtents(qcow2)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(full, actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
	t.Run("qcow2 zlib", func(t *testing.T) {
		qcow2Zlib := qcow2 + ".zlib"
		if err := qemuimg.Convert(qcow2, qcow2Zlib, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			t.Fatal(err)
		}
		actual, err := listExtents(qcow2Zlib)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(compressed(full), actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
}

func TestExtentsMerge(t *testing.T) {
	// Create image with consecutive extents of same type.
	extents := []image.Extent{
		{Start: 0 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 1 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 2 * clusterSize, Length: 98 * clusterSize, Zero: true},
		{Start: 100 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 101 * clusterSize, Length: 1 * clusterSize, Allocated: true},
	}

	// Extents with same type are merged.
	merged := []image.Extent{
		{Start: 0 * clusterSize, Length: 2 * clusterSize, Allocated: true},
		{Start: 2 * clusterSize, Length: 98 * clusterSize, Zero: true},
		{Start: 100 * clusterSize, Length: 2 * clusterSize, Allocated: true},
	}

	qcow2 := filepath.Join(t.TempDir(), "image")
	if err := createTestImageWithExtents(qcow2, qemuimg.FormatQcow2, extents, "", ""); err != nil {
		t.Fatal(err)
	}
	t.Run("qcow2", func(t *testing.T) {
		actual, err := listExtents(qcow2)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(merged, actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
	t.Run("qcow2 zlib", func(t *testing.T) {
		qcow2Zlib := qcow2 + ".zlib"
		if err := qemuimg.Convert(qcow2, qcow2Zlib, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			t.Fatal(err)
		}
		actual, err := listExtents(qcow2Zlib)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(compressed(merged), actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
}

func TestExtentsZero(t *testing.T) {
	// Create image with different clusters that read as zeros.
	extents := []image.Extent{
		{Start: 0 * clusterSize, Length: 1000 * clusterSize, Allocated: true, Zero: true},
		{Start: 1000 * clusterSize, Length: 1000 * clusterSize, Zero: true},
	}

	qcow2 := filepath.Join(t.TempDir(), "image")
	if err := createTestImageWithExtents(qcow2, qemuimg.FormatQcow2, extents, "", ""); err != nil {
		t.Fatal(err)
	}
	t.Run("qcow2", func(t *testing.T) {
		actual, err := listExtents(qcow2)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(extents, actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
	t.Run("qcow2 zlib", func(t *testing.T) {
		qcow2Zlib := qcow2 + ".zlib"
		if err := qemuimg.Convert(qcow2, qcow2Zlib, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			t.Fatal(err)
		}
		actual, err := listExtents(qcow2Zlib)
		if err != nil {
			t.Fatal(err)
		}
		// When converting to qcow2 images all clusters that read as zeros are
		// converted to unallocated clusters.
		converted := []image.Extent{
			{Start: 0 * clusterSize, Length: 2000 * clusterSize, Zero: true},
		}
		if !slices.Equal(converted, actual) {
			t.Fatalf("expected %v, got %v", extents, actual)
		}
	})
}

func TestExtentsBackingFile(t *testing.T) {
	// Create an image with some clusters in the backing file, and some cluasters
	// in the image. Accessing extents should present a unified view using both
	// image and backing file.
	tmpDir := t.TempDir()
	baseExtents := []image.Extent{
		{Start: 0 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 1 * clusterSize, Length: 9 * clusterSize, Zero: true},
		{Start: 10 * clusterSize, Length: 2 * clusterSize, Allocated: true},
		{Start: 12 * clusterSize, Length: 88 * clusterSize, Zero: true},
		{Start: 100 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 101 * clusterSize, Length: 899 * clusterSize, Zero: true},
	}
	topExtents := []image.Extent{
		{Start: 0 * clusterSize, Length: 1 * clusterSize, Zero: true},
		{Start: 1 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		{Start: 2 * clusterSize, Length: 9 * clusterSize, Zero: true},
		{Start: 11 * clusterSize, Length: 2 * clusterSize, Allocated: true},
		{Start: 13 * clusterSize, Length: 986 * clusterSize, Zero: true},
		{Start: 999 * clusterSize, Length: 1 * clusterSize, Allocated: true},
	}
	baseRaw := filepath.Join(tmpDir, "base.raw")
	if err := createTestImageWithExtents(baseRaw, qemuimg.FormatRaw, baseExtents, "", ""); err != nil {
		t.Fatal(err)
	}

	t.Run("qcow2", func(t *testing.T) {
		tmpDir := t.TempDir()
		baseQcow2 := filepath.Join(tmpDir, "base.qcow2")
		if err := qemuimg.Convert(baseRaw, baseQcow2, qemuimg.FormatQcow2, qemuimg.CompressionNone); err != nil {
			t.Fatal(err)
		}
		top := filepath.Join(tmpDir, "top.qcow2")
		if err := createTestImageWithExtents(top, qemuimg.FormatQcow2, topExtents, baseQcow2, qemuimg.FormatQcow2); err != nil {
			t.Fatal(err)
		}
		// When top and base are uncompressed, extents from to and based are merged.
		expected := []image.Extent{
			{Start: 0 * clusterSize, Length: 2 * clusterSize, Allocated: true},
			{Start: 2 * clusterSize, Length: 8 * clusterSize, Zero: true},
			{Start: 10 * clusterSize, Length: 3 * clusterSize, Allocated: true},
			{Start: 13 * clusterSize, Length: 87 * clusterSize, Zero: true},
			{Start: 100 * clusterSize, Length: 1 * clusterSize, Allocated: true},
			{Start: 101 * clusterSize, Length: 898 * clusterSize, Zero: true},
			{Start: 999 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		}
		actual, err := listExtents(top)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(expected, actual) {
			t.Fatalf("expected %v, got %v", expected, actual)
		}
	})
	t.Run("qcow2 zlib", func(t *testing.T) {
		tmpDir := t.TempDir()
		baseQcow2Zlib := filepath.Join(tmpDir, "base.qcow2")
		if err := qemuimg.Convert(baseRaw, baseQcow2Zlib, qemuimg.FormatQcow2, qemuimg.CompressionZlib); err != nil {
			t.Fatal(err)
		}
		top := filepath.Join(tmpDir, "top.qcow2")
		if err := createTestImageWithExtents(top, qemuimg.FormatQcow2, topExtents, baseQcow2Zlib, qemuimg.FormatQcow2); err != nil {
			t.Fatal(err)
		}
		// When base is compressed, extents from to and based cannot be merged since
		// allocated extents from base are compressed. When copying we can merge
		// extents with different types that read as zero.
		expected := []image.Extent{
			// From base
			{Start: 0 * clusterSize, Length: 1 * clusterSize, Allocated: true, Compressed: true},
			// From top
			{Start: 1 * clusterSize, Length: 1 * clusterSize, Allocated: true},
			{Start: 2 * clusterSize, Length: 8 * clusterSize, Zero: true},
			// From base
			{Start: 10 * clusterSize, Length: 1 * clusterSize, Allocated: true, Compressed: true},
			// From top (top clusters hide base clusters)
			{Start: 11 * clusterSize, Length: 2 * clusterSize, Allocated: true},
			{Start: 13 * clusterSize, Length: 87 * clusterSize, Zero: true},
			// From base
			{Start: 100 * clusterSize, Length: 1 * clusterSize, Allocated: true, Compressed: true},
			{Start: 101 * clusterSize, Length: 898 * clusterSize, Zero: true},
			// From top
			{Start: 999 * clusterSize, Length: 1 * clusterSize, Allocated: true},
		}
		actual, err := listExtents(top)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(expected, actual) {
			t.Fatalf("expected %v, got %v", expected, actual)
		}
	})
}

func TestExtentsBackingFileShort(t *testing.T) {
	// Test the special case of shorter backing file. The typical use case is
	// adding a large qcow2 image on top of a small os image.
	baseExtents := []image.Extent{
		{Start: 0 * clusterSize, Length: 10 * clusterSize, Allocated: true},
		{Start: 10 * clusterSize, Length: 90 * clusterSize, Zero: true},
	}
	topExtents := []image.Extent{
		{Start: 0 * clusterSize, Length: 5 * clusterSize, Zero: true},
		{Start: 5 * clusterSize, Length: 10 * clusterSize, Allocated: true},
		{Start: 15 * clusterSize, Length: 984 * clusterSize, Zero: true},
		{Start: 999 * clusterSize, Length: 1 * clusterSize, Allocated: true},
	}
	expected := []image.Extent{
		{Start: 0 * clusterSize, Length: 15 * clusterSize, Allocated: true},
		{Start: 15 * clusterSize, Length: 984 * clusterSize, Zero: true},
		{Start: 999 * clusterSize, Length: 1 * clusterSize, Allocated: true},
	}
	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "base.qcow2")
	if err := createTestImageWithExtents(base, qemuimg.FormatQcow2, baseExtents, "", ""); err != nil {
		t.Fatal(err)
	}
	top := filepath.Join(tmpDir, "top.qcow2")
	if err := createTestImageWithExtents(top, qemuimg.FormatQcow2, topExtents, base, qemuimg.FormatQcow2); err != nil {
		t.Fatal(err)
	}
	actual, err := listExtents(top)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(expected, actual) {
		t.Fatalf("expected %v, got %v", expected, actual)
	}
}

func TestExtentsBackingFileShortUnaligned(t *testing.T) {
	// Test the special case of raw backing file not aligned to cluster size. When
	// getting a short extent from the end of the backing file, we aligned the
	// extent length to the top image cluster size.
	blockSize := 4 * KiB
	baseExtents := []image.Extent{
		{Start: 0, Length: 100 * blockSize, Allocated: true},
	}
	topExtents := []image.Extent{
		{Start: 0, Length: 10 * clusterSize, Zero: true},
	}
	expected := []image.Extent{
		{Start: 0 * clusterSize, Length: 7 * clusterSize, Allocated: true},
		{Start: 7 * clusterSize, Length: 3 * clusterSize, Zero: true},
	}
	tmpDir := t.TempDir()
	base := filepath.Join(tmpDir, "base.raw")
	if err := createTestImageWithExtents(base, qemuimg.FormatRaw, baseExtents, "", ""); err != nil {
		t.Fatal(err)
	}
	top := filepath.Join(tmpDir, "top.qcow2")
	if err := createTestImageWithExtents(top, qemuimg.FormatQcow2, topExtents, base, qemuimg.FormatRaw); err != nil {
		t.Fatal(err)
	}
	actual, err := listExtents(top)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(expected, actual) {
		t.Fatalf("expected %v, got %v", expected, actual)
	}
}

func compressed(extents []image.Extent) []image.Extent {
	var res []image.Extent
	for _, extent := range extents {
		if extent.Allocated {
			extent.Compressed = true
		}
		res = append(res, extent)
	}
	return res
}

func listExtents(path string) ([]image.Extent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := qcow2reader.Open(f)
	if err != nil {
		return nil, err
	}
	defer img.Close()

	var extents []image.Extent
	var start int64

	end := img.Size()
	for start < end {
		extent, err := img.Extent(start, end-start)
		if err != nil {
			return nil, err
		}
		if extent.Start != start {
			return nil, fmt.Errorf("invalid extent start: %+v", extent)
		}
		if extent.Length <= 0 {
			return nil, fmt.Errorf("invalid extent length: %+v", extent)
		}
		extents = append(extents, extent)
		start += extent.Length
	}
	return extents, nil
}

// createTestImageWithExtents creates a n image with the allocation described
// by extents.
func createTestImageWithExtents(
	path string,
	format qemuimg.Format,
	extents []image.Extent,
	backingFile string,
	backingFormat qemuimg.Format,
) error {
	lastExtent := extents[len(extents)-1]
	size := lastExtent.Start + lastExtent.Length
	if err := qemuimg.Create(path, format, size, backingFile, backingFormat); err != nil {
		return err
	}
	for _, extent := range extents {
		if !extent.Allocated {
			continue
		}
		start := extent.Start
		length := extent.Length
		for length > 0 {
			// qemu-io requires length < 2g.
			n := length
			if n >= 2*GiB {
				n = 2*GiB - 64*KiB
			}
			if extent.Zero {
				if err := qemuio.Zero(path, format, start, n); err != nil {
					return err
				}
			} else {
				if err := qemuio.Write(path, format, start, n, 0x55); err != nil {
					return err
				}
			}
			start += n
			length -= n
		}
	}
	return nil
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
	err = c.Convert(dst, img, img.Size(), nil)
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
