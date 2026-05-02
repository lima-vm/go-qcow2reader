package convert

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/lima-vm/go-qcow2reader/image"
)

// The size of the buffer used to read data from non-zero extents of the image.
// For best performance, the size should be aligned to the image cluster size or
// the file system block size.
const BufferSize = 1024 * 1024

// To maxmimze performance we use multiple goroutines to read data from the
// source image, decompress data, and write data to the target image. To
// schedule the work to multiple goroutines, the image is split to multiple
// segments, each processed by a single worker goroutine.
//
// Smaller value may increase the overhead of synchornizing multiple workers.
// Larger value may be less efficient for smaller images. The default value
// gives good results for the lima default Ubuntu image. Must be aligned to
// BufferSize.
const SegmentSize = 32 * BufferSize

// For best I/O throughput we want to have enough in-flight requests, regardless
// of number of cores. For best decompression we want to use one worker per
// core, but too many workers are less effective. The default value gives good
// results with lima default Ubuntu image.
const Workers = 8

// Updater is an interface for tracking conversion progress.
type Updater interface {
	// Called from multiple goroutines after a byte range of length was converted.
	// If the conversion is successfu, the total number of bytes will be the image
	// virtual size.
	Update(n int64)
}

type Options struct {
	// SegmentSize in bytes. Must be aligned to BufferSize. If not set, use the
	// default value (32 MiB).
	SegmentSize int64

	// BufferSize in bytes. If not set, use the default value (1 MiB).
	BufferSize int

	// Workers is the number of goroutines copying buffers in parallel. If not set
	// use the default value (8).
	Workers int

	// If set, update progress during conversion.
	Progress Updater
}

// Validate validates options and set default values. Returns an error for
// invalid option values.
func (o *Options) Validate() error {
	if o.SegmentSize < 0 {
		return errors.New("segment size must be positive")
	}
	if o.SegmentSize == 0 {
		o.SegmentSize = SegmentSize
	}

	if o.BufferSize < 0 {
		return errors.New("buffer size must be positive")
	}
	if o.BufferSize == 0 {
		o.BufferSize = BufferSize
	}

	if o.Workers < 0 {
		return errors.New("number of workers must be positive")
	}
	if o.Workers == 0 {
		o.Workers = Workers
	}

	// This is not stritcly required, but there is no reason support unaligned
	// segment size.
	if o.SegmentSize%int64(o.BufferSize) != 0 {
		return errors.New("segment size not aligned to buffer size")
	}

	return nil
}

type conversion struct {
	// Read only.
	size        int64
	segmentSize int64
	zero        []byte
	wa          io.WriterAt
	img         image.Image

	// Modified during Convert, protected by the mutex.
	mutex  sync.Mutex
	offset int64
	err    error
}

// nextSegment returns the next segment to process and stop flag. The stop flag
// is true if there is no more work, or if another workers has failed and set
// the error.
func (c *conversion) nextSegment() (int64, int64, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.offset == c.size || c.err != nil {
		return 0, 0, true
	}

	start := c.offset
	c.offset += c.segmentSize
	if c.offset > c.size {
		c.offset = c.size
	}

	return start, c.offset, false
}

// setError keeps the first error set. Setting the error signal other workers to
// abort the operation.
func (c *conversion) setError(err error) {
	c.mutex.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mutex.Unlock()
}

// Convert copy image to io.WriterAt. Unallocated extents in the image or read
// data which is all zeros are converted to unallocated byte range in the target
// image. The target image must be new empty file or a file full of zeroes. To
// get a sparse target image, the image must be a new empty file, since Convert
// does not punch holes for zero ranges even if the underlying file system
// supports hole punching.
func Convert(wa io.WriterAt, img image.Image, opts Options) error {
	if err := opts.Validate(); err != nil {
		return err
	}
	conv := conversion{
		size:        img.Size(),
		segmentSize: opts.SegmentSize,
		zero:        make([]byte, opts.BufferSize),
		wa:          wa,
		img:         img,
	}
	var wg sync.WaitGroup

	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := worker{
				conv: &conv,
				opts: &opts,
				buf:  make([]byte, opts.BufferSize),
			}
			if err := w.run(); err != nil {
				conv.setError(err)
			}
		}()
	}

	wg.Wait()
	return conv.err
}

type worker struct {
	conv *conversion
	opts *Options
	buf  []byte
}

func (w *worker) run() error {
	for {
		start, end, stop := w.conv.nextSegment()
		if stop {
			return nil
		}

		for start < end {
			extent, err := w.conv.img.Extent(start, end-start)
			if err != nil {
				return err
			}

			if extent.Zero {
				w.zeroExtent(extent)
			} else if err := w.copyExtent(extent); err != nil {
				return err
			}

			start += extent.Length
		}
	}
}

// zeroExtent ensures that the extent is full of zeros in the target. Currently
// a no-op since we assume a new sparse file, but should punch a hole in the
// future.
func (w *worker) zeroExtent(extent image.Extent) {
	if w.opts.Progress != nil {
		w.opts.Progress.Update(extent.Length)
	}
}

// copyExtent copies data from the image to the target writer. Ranges that read
// as all zeros are skipped to keep the target sparse.
func (w *worker) copyExtent(extent image.Extent) error {
	for extent.Length > 0 {
		n := len(w.buf)
		if extent.Length < int64(len(w.buf)) {
			n = int(extent.Length)
		}

		nr, err := w.conv.img.ReadAt(w.buf[:n], extent.Start)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return err
			}

			// EOF for the last read of the last segment is expected, but since we
			// read exactly size bytes, we should never get a zero read.
			if nr == 0 {
				return errors.New("unexpected EOF")
			}
		}

		if !bytes.Equal(w.buf[:nr], w.conv.zero[:nr]) {
			if nw, err := w.conv.wa.WriteAt(w.buf[:nr], extent.Start); err != nil {
				return err
			} else if nw != nr {
				return fmt.Errorf("read %d, but wrote %d bytes", nr, nw)
			}
		}

		if w.opts.Progress != nil {
			w.opts.Progress.Update(int64(nr))
		}

		extent.Length -= int64(nr)
		extent.Start += int64(nr)
	}
	return nil
}
