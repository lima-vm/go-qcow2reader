package vmdk

import (
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/stub"
)

const Type = image.Type("vmdk")

// Open returns a stub.
func Open(ra io.ReaderAt) (*stub.Stub, error) {
	return stub.New(ra, Type,
		stub.SimpleProber([]byte("# Disk DescriptorFile")),
		stub.SimpleProber([]byte("KDMV")), // vmdk4
		stub.SimpleProber([]byte("COWD")), // vmdk3
	)
}
