package qcow2reader

import (
	"errors"
	"fmt"
	"io"

	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/parallels"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
	"github.com/lima-vm/go-qcow2reader/image/raw"
	"github.com/lima-vm/go-qcow2reader/image/vdi"
	"github.com/lima-vm/go-qcow2reader/image/vhdx"
	"github.com/lima-vm/go-qcow2reader/image/vmdk"
	"github.com/lima-vm/go-qcow2reader/image/vpc"
)

// Types is the known image types.
var Types = []image.Type{
	qcow2.Type,
	vmdk.Type,
	vhdx.Type,
	vdi.Type,
	parallels.Type,
	vpc.Type,
	raw.Type, // raw must be the last type
}

// Open opens an image.
func Open(ra io.ReaderAt) (image.Image, error) {
	for _, t := range Types {
		img, err := OpenWithType(ra, t)
		if err == nil {
			return img, nil
		}
		if !errors.Is(err, image.ErrWrongType) {
			err = fmt.Errorf("failed to open the image as %q: %w", t, err)
			return img, err
		}
	}
	return OpenWithType(ra, raw.Type)
}

// OpenWithType open opens an image with the specified [image.Type].
func OpenWithType(ra io.ReaderAt, t image.Type) (image.Image, error) {
	switch t {
	case "":
		return Open(ra)
	case qcow2.Type:
		return qcow2.Open(ra, OpenWithType)
	case vmdk.Type:
		return vmdk.Open(ra)
	case vhdx.Type:
		return vhdx.Open(ra)
	case vdi.Type:
		return vdi.Open(ra)
	case parallels.Type:
		return parallels.Open(ra)
	case vpc.Type:
		return vpc.Open(ra)
	case raw.Type:
		return raw.Open(ra)
	default:
		return nil, fmt.Errorf("unknown type: %q", t)
	}
}
