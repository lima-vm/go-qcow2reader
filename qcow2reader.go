package qcow2reader

import (
	"errors"
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
	"github.com/lima-vm/go-qcow2reader/image/raw"
)

// Open opens an image.
func Open(ra io.ReaderAt) (image.Image, error) {
	q, err := qcow2.Open(ra, Open)
	if errors.Is(err, qcow2.ErrNotQcow2) {
		return raw.Open(ra)
	}
	return q, err
}
