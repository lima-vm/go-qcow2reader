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
	bufToSectorCount := make([]byte, 8)
	if _, err := ra.ReadAt(bufToSectorCount, 48); err != nil {
		return nil, err
	}
	sectorCount := binary.BigEndian.Uint64(bufToSectorCount)
	// Block size
	// ref: https://github.com/fox-it/dissect.hypervisor/blob/0c8976613a369923e69022304b2f0ed587e997e2/dissect/hypervisor/disk/c_asif.py#L19
	bufToBlockSize := make([]byte, 2)
	if _, err := ra.ReadAt(bufToBlockSize, 68); err != nil {
		return nil, err
	}
	blockSize := binary.BigEndian.Uint16(bufToBlockSize)
	return &Asif{
		sectorCount: sectorCount,
		blockSize:   blockSize,
		Stub:        *stub,
	}, nil
}

type Asif struct {
	sectorCount uint64
	blockSize   uint16
	stub.Stub
}

var _ image.Image = (*Asif)(nil)

func (a *Asif) Size() int64 {
	return int64(a.sectorCount) * int64(a.blockSize)
}
