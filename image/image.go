package image

import (
	"errors"
	"io"
)

// Type must be a "Backing file format name string" that appears in QCOW2.
type Type string

// Image implements [io.ReaderAt] and [io.Closer].
type Image interface {
	io.ReaderAt
	io.Closer
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
