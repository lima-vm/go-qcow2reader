package raw

import (
	"errors"
	"io"
	"os"

	"github.com/lima-vm/go-qcow2reader/image"
)

const Type = image.Type("raw")

// Raw implements [image.Image].
type Raw struct {
	io.ReaderAt `json:"-"`
}

// Extent returns an allocated extent starting at the specified offset with
// specified length. It is used when the speicfic image type does not implement
// Extent(). The implementation is correct but inefficient. Fails if image size
// is unknown.
func (img *Raw) Extent(start, length int64) (image.Extent, error) {
	if start+length > img.Size() {
		return image.Extent{}, errors.New("length out of bounds")
	}
	// TODO: Implement using SEEK_HOLE/SEEK_DATA when supported by the file system.
	return image.Extent{Start: start, Length: length, Allocated: true}, nil
}

func (img *Raw) Close() error {
	if closer, ok := img.ReaderAt.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (img *Raw) Type() image.Type {
	return Type
}

func (img *Raw) Size() int64 {
	if f, ok := img.ReaderAt.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			return st.Size()
		}
	}
	return -1
}

func (img *Raw) Readable() error {
	return nil
}

// Open opens a raw image.
func Open(ra io.ReaderAt) (*Raw, error) {
	return &Raw{ReaderAt: ra}, nil
}
