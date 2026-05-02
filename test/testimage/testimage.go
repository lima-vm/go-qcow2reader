package testimage

import (
	"fmt"
	"io"
	"math/rand"
	"os"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/test/qemuimg"
	"github.com/lima-vm/go-qcow2reader/test/qemuio"
	. "github.com/lima-vm/go-qcow2reader/test/units" //nolint:staticcheck
)

// CreateWithExtents creates an image with the allocation described by extents.
func CreateWithExtents(
	path string,
	format qemuimg.Format,
	extents []image.Extent,
	backingFile string,
	backingFormat qemuimg.Format,
) error {
	if len(extents) == 0 {
		return fmt.Errorf("empty extents")
	}
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

// Create creates a raw image with fake data that compresses like real image data.
// Utilization determines the amount of data to allocate (0.0--1.0).
func Create(filename string, size int64, utilization float64) error {
	if utilization < 0 || utilization > 1 {
		return fmt.Errorf("utilization out of range (0.0-1.0): %f", utilization)
	}

	const chunkSize = 8 * MiB
	dataSize := int64(float64(chunkSize) * utilization)

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck
	if err := file.Truncate(size); err != nil {
		return err
	}
	if dataSize > 0 {
		reader := &generator{}
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

// generator generates fake data that compresses like real image data (30%).
type generator struct{}

func (g *generator) Read(b []byte) (int, error) {
	for i := 0; i < len(b); i++ {
		b[i] = byte(i & 0xff)
	}
	rand.Shuffle(len(b)/8*5, func(i, j int) {
		b[i], b[j] = b[j], b[i]
	})
	return len(b), nil
}
