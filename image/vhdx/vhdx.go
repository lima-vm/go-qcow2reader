package vhdx

import (
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/stub"
)

const Type = image.Type("vhdx")

// Open returns a stub.
func Open(ra io.ReaderAt) (*stub.Stub, error) {
	return stub.New(ra, Type, stub.SimpleProber([]byte("vhdxfile")))
}
