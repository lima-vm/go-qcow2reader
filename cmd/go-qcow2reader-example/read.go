package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/log"
)

func cmdRead(args []string) error {
	var (
		// Required
		filename string

		// Options
		debug      bool
		bufferSize int
		offset     int64
		length     int64
	)

	fs := flag.NewFlagSet("read", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s read [OPTIONS...] FILE\n", os.Args[0])
		flag.PrintDefaults()
	}
	fs.BoolVar(&debug, "debug", false, "enable printing debug messages")
	fs.IntVar(&bufferSize, "buffer-size", 65536, "buffer size")
	fs.Int64Var(&offset, "offset", 0, "offset to read")
	fs.Int64Var(&length, "length", -1, "length to read")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if debug {
		log.SetDebugFunc(logDebug)
	}

	switch len(fs.Args()) {
	case 0:
		return errors.New("no file was specified")
	case 1:
		filename = fs.Arg(0)
	default:
		return errors.New("too many files were specified")
	}

	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	img, err := qcow2reader.Open(f)
	if err != nil {
		return err
	}
	defer img.Close()

	if length < 0 {
		length = img.Size()
	}

	buf := make([]byte, bufferSize)
	sr := io.NewSectionReader(img, offset, length)
	w := &hideReadFrom{os.Stdout}

	_, err = io.CopyBuffer(w, sr, buf)

	return err
}

// hideReadFrom hides os.File ReadFrom method to ensure that io.CopyBuffer()
// will use our buffer, speeding up the copy significantly. For more info see
// https://github.com/lima-vm/go-qcow2reader/pull/42.
type hideReadFrom struct {
	f *os.File
}

func (w *hideReadFrom) Write(p []byte) (int, error) {
	return w.f.Write(p)
}
