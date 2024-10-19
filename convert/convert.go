package convert

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"
)

const BufferSize = 1024 * 1024

// Smaller value may increase the overhead of synchornizing multiple works.
// Larger value may be less efficient for smaller images. The defualt value
// gives good results for the lima default Ubuntu image.
const SegmentSize = 32 * BufferSize

// For best I/O throughput we want to have enough in-flight requests, regardless
// of number of cores. For best decompression we want to use one worker per
// core, but too many workers is less effective. The default value gives good
// results with lima default Ubuntu image.
const Workers = 8

type Options struct {
	// SegmentSize in bytes. Must be aligned to BufferSize. If not set, use the
	// default value (32 MiB).
	SegmentSize int64

	// BufferSize in bytes. If not set, use the default value (1 MiB).
	BufferSize int

	// Workers is the number of goroutines copying buffers in parallel. If not set
	// use the default value (8).
	Workers int
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

type Converter struct {
	// Read only after starting.
	size        int64
	segmentSize int64
	bufferSize  int
	workers     int

	// State modified during Convert, protected by the mutex.
	mutex  sync.Mutex
	offset int64
	err    error
}

// New returns a new converter intialized from options.
func New(opts Options) (*Converter, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	c := &Converter{
		segmentSize: opts.SegmentSize,
		bufferSize:  opts.BufferSize,
		workers:     opts.Workers,
	}
	return c, nil
}

// nextSegment returns the next segment to process and stop flag. The stop flag
// is true if there is no more work, or if another workers has failed and set
// the error.
func (c *Converter) nextSegment() (int64, int64, bool) {
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

// setError keeps the first error set. Setting the error signal other workes to
// abort the operation.
func (c *Converter) setError(err error) {
	c.mutex.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mutex.Unlock()
}

func (c *Converter) reset(size int64) {
	c.size = size
	c.err = nil
	c.offset = 0
}

// Convert copy size bytes from io.ReaderAt to io.WriterAt. Unallocated areas or
// areas full of zeros in the source are keep unallocated in the destination.
// The destination must be new empty or full of zeroes.
func (c *Converter) Convert(wa io.WriterAt, ra io.ReaderAt, size int64) error {
	c.reset(size)

	zero := make([]byte, c.bufferSize)
	var wg sync.WaitGroup

	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, c.bufferSize)
			for {
				// Get next segment to copy.
				start, end, stop := c.nextSegment()
				if stop {
					return
				}

				for start < end {
					// The last read may be shorter.
					n := len(buf)
					if end-start < int64(len(buf)) {
						n = int(end - start)
					}

					// Read more data.
					nr, err := ra.ReadAt(buf[:n], start)
					if err != nil {
						if !errors.Is(err, io.EOF) {
							c.setError(err)
							return
						}

						// EOF for the last read of the last segment is expected, but since we
						// read exactly size bytes, we shoud never get a zero read.
						if nr == 0 {
							c.setError(errors.New("unexpected EOF"))
							return
						}
					}

					// If the data is all zeros we skip it to create a hole. Otherwise
					// write the data.
					if !bytes.Equal(buf[:nr], zero[:nr]) {
						if nw, err := wa.WriteAt(buf[:nr], start); err != nil {
							c.setError(err)
							return
						} else if nw != nr {
							c.setError(fmt.Errorf("read %d, but wrote %d bytes", nr, nw))
							return
						}
					}
					start += int64(nr)
				}
			}
		}()
	}

	wg.Wait()
	return c.err
}
