package main

import (
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
	"github.com/lima-vm/go-qcow2reader/log"
)

func logWarn(s string) {
	fmt.Fprintln(os.Stderr, "WARNING: "+s)
}

func logDebug(s string) {
	fmt.Fprintln(os.Stderr, "DEBUG: "+s)
}

type zstdDecompressor struct {
	*zstd.Decoder
}

func (x *zstdDecompressor) Close() error {
	x.Decoder.Close()
	return nil
}

func newZstdDecompressor(r io.Reader) (io.ReadCloser, error) {
	dec, err := zstd.NewReader(r)
	if err != nil {
		return nil, err
	}
	return &zstdDecompressor{dec}, nil
}

func usage() {
	usage := `Usage: %s COMMAND [OPTIONS...]

Available commands:
  info		show image information
  read		read image data and print to stdout
`
	fmt.Fprintf(os.Stderr, usage, os.Args[0])
	os.Exit(1)
}

func main() {
	log.SetWarnFunc(logWarn)

	// zlib (deflate) decompressor is registered by default, but zstd is not.
	qcow2.SetDecompressor(qcow2.CompressionTypeZstd, newZstdDecompressor)

	var err error

	var cmd string
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var args []string
	if len(os.Args) > 2 {
		args = os.Args[2:]
	}

	switch cmd {
	case "info":
		err = cmdInfo(args)
	case "read":
		err = cmdRead(args)
	default:
		usage()
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
		os.Exit(1)
	}
}
