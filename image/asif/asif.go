package asif

import (
	"encoding/binary"
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/stub"
)

const Type = image.Type("asif")

// Open returns an ASIF image.
func Open(ra io.ReaderAt) (*Asif, error) {
	stub, err := stub.New(ra, Type, stub.SimpleProber([]byte("shdw")))
	if err != nil {
		return nil, err
	}
	// Block count seems to be stored at offset 48 as a big-endian uint64.
	buf := make([]byte, 8)
	if _, err := ra.ReadAt(buf, 48); err != nil {
		return nil, err
	}
	blocks := binary.BigEndian.Uint64(buf)
	return &Asif{
		// Block size is 512 bytes.
		// It might be stored in the header, but for now we assume it's 512 bytes.
		size: int64(blocks) * 512,
		Stub: *stub,
	}, nil
}

type Asif struct {
	size int64
	stub.Stub
}

var _ image.Image = (*Asif)(nil)

func (a *Asif) Size() int64 {
	return a.size
}
