package raw

import (
	"io"
	"os"

	"github.com/lima-vm/go-qcow2reader/image"
)

const Type = image.Type("raw")

// Raw implements [image.Image].
type Raw struct {
	io.ReaderAt `json:"-"`
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
