package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/image/qcow2"
	"github.com/lima-vm/go-qcow2reader/log"
)

func warn(s string) {
	fmt.Fprintln(os.Stderr, "WARNING: "+s)
}

func debugPrint(s string) {
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

func main() {
	log.SetWarnFunc(warn)

	// zlib (deflate) decompressor is registered by default, but zstd is not.
	qcow2.SetDecompressor(qcow2.CompressionTypeZstd, newZstdDecompressor)

	if err := xmain(); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
		os.Exit(1)
	}
}

func xmain() error {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [OPTIONS...] FILE\n", os.Args[0])
		flag.PrintDefaults()
	}
	var (
		debug      bool
		info       bool
		bufferSize int
		offset     int64
		length     int64
	)
	flag.BoolVar(&debug, "debug", false, "enable printing debug messages")
	flag.BoolVar(&info, "info", false, "print the image info and exit")
	flag.IntVar(&bufferSize, "buffer", 65536, "buffer size")
	flag.Int64Var(&offset, "offset", 0, "offset to read")
	flag.Int64Var(&length, "length", -1, "length to read")
	flag.Parse()
	if debug {
		log.SetDebugFunc(debugPrint)
	}

	args := flag.Args()
	switch len(args) {
	case 0:
		return errors.New("no file was specified")
	case 1:
		// NOP
	default:
		return errors.New("too many files were specified")
	}
	fName := args[0]

	f, err := os.Open(fName)
	if err != nil {
		return err
	}
	defer f.Close()

	img, err := qcow2reader.Open(f)
	if err != nil {
		return err
	}

	if info {
		imgInfo := image.NewImageInfo(img)
		j, err := json.MarshalIndent(imgInfo, "", "    ")
		if err != nil {
			return err
		}
		if _, err = fmt.Println(string(j)); err != nil {
			return err
		}
		if err = img.Readable(); err != nil {
			warn(err.Error())
		}
		return nil
	}

	if length < 0 {
		length = img.Size()
	}
	buf := make([]byte, bufferSize)
	sr := io.NewSectionReader(img, offset, length)
	_, err = io.CopyBuffer(os.Stdout, sr, buf)
	return err
}
