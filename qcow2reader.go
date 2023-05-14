package qcow2reader

import (
	"errors"
	"fmt"
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
	"github.com/lima-vm/go-qcow2reader/image/raw"
)

// Open opens an image.
func Open(ra io.ReaderAt) (image.Image, error) {
	q, err := qcow2.Open(ra, OpenWithType)
	if errors.Is(err, qcow2.ErrNotQcow2) {
		return raw.Open(ra)
	}
	return q, err
}

func OpenWithType(ra io.ReaderAt, t image.Type) (image.Image, error) {
	switch t {
	case "":
		return Open(ra)
	case qcow2.Type:
		return qcow2.Open(ra, OpenWithType)
	case raw.Type:
		return raw.Open(ra)
	default:
		return nil, fmt.Errorf("unknown type: %q", t)
	}
}
