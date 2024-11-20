package image

import (
	"errors"
	"io"
)

// Type must be a "Backing file format name string" that appears in QCOW2.
type Type string

// Extent describes a byte range in the image with the same allocation,
// compression, or zero status. Extents are aligned to the underlying file
// system block size (e.g. 4k), or the image format cluster size (e.g. 64k). One
// extent can describe one or more file system blocks or image clusters.
type Extent struct {
	// Offset from start of the image in bytes.
	Start int64 `json:"start"`
	// Length of this extent in bytes.
	Length int64 `json:"length"`
	// Set if this extent is allocated.
	Allocated bool `json:"allocated"`
	// Set if this extent is read as zeros.
	Zero bool `json:"zero"`
	// Set if this extent is compressed.
	Compressed bool `json:"compressed"`
}

// Image implements [io.ReaderAt] and [io.Closer].
type Image interface {
	io.ReaderAt
	io.Closer
	Extent(start, length int64) (Extent, error)
	Type() Type
	Size() int64 // -1 if unknown
	Readable() error
}

// ErrWrongType is returned from [Opener].
var ErrWrongType = errors.New("wrong image type")

// OpenWithType opens [Image] with the specified [Type].
// Opener must return [ErrWrongType] when the image is not parsable with
// the specified [Type].
type OpenWithType func(io.ReaderAt, Type) (Image, error)

// ImageInfo wraps [Image] for [json.Marshal].
type ImageInfo struct {
	Type  Type  `json:"type"`
	Size  int64 `json:"size"`
	Image `json:"image"`
}

// NewImageInfo returns image info.
func NewImageInfo(img Image) *ImageInfo {
	return &ImageInfo{
		Type:  img.Type(),
		Size:  img.Size(),
		Image: img,
	}
}
