package image

import (
	"io"
)

type Type string

// Image implements [io.ReaderAt] and [io.Closer].
type Image interface {
	io.ReaderAt
	io.Closer
	Type() Type
	Size() int64 // -1 if unknown
	Readable() error
}

type Opener func(ra io.ReaderAt) (Image, error)

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
