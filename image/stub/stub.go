package stub

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
)

// Prober returns true if the sector has the desired format.
// The length of sector is usually 512.
type Prober func(sector []byte) bool

// SimpleProber provides a simple [Prober].
func SimpleProber(magic []byte) Prober {
	return func(sector []byte) bool {
		return bytes.HasPrefix(sector, magic)
	}
}

// Stub implements [image.Image].
type Stub struct {
	t image.Type
}

func (img *Stub) ReadAt([]byte, int64) (int, error) {
	return 0, img.Readable()
}

func (img *Stub) Close() error {
	return nil
}

func (img *Stub) Type() image.Type {
	return img.t
}

func (img *Stub) Size() int64 {
	return -1
}

func (img *Stub) Readable() error {
	return fmt.Errorf("unimplemented type: %q", img.t)
}

// New creates a stub.
func New(ra io.ReaderAt, t image.Type, probers ...Prober) (*Stub, error) {
	sector := make([]byte, 512)
	if _, err := ra.ReadAt(sector, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to read the first %d bytes: %w", len(sector), err)
	}
	for _, probe := range probers {
		if probe(sector) {
			stub := &Stub{
				t: t,
			}
			return stub, nil
		}
	}
	return nil, fmt.Errorf("%w: image is not %s", image.ErrWrongType, t)
}
