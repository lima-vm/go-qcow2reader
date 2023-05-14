package vdi

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/stub"
)

const Type = image.Type("vdi")

// Open returns a stub.
func Open(ra io.ReaderAt) (*stub.Stub, error) {
	prober := func(b []byte) bool {
		sr := io.NewSectionReader(bytes.NewReader(b), 0x40, 4)
		var magic uint32
		if err := binary.Read(sr, binary.LittleEndian, &magic); err != nil {
			return false
		}
		return magic == 0xbeda107f
	}
	return stub.New(ra, Type, prober)
}
