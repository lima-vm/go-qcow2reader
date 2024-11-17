package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/convert"
	"github.com/lima-vm/go-qcow2reader/log"
)

func cmdConvert(args []string) error {
	var (
		// Required
		source, target string

		// Options
		debug   bool
		options convert.Options
	)

	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s convert [OPTIONS...] SOURCE TARGET\n", os.Args[0])
		flag.PrintDefaults()
	}
	fs.BoolVar(&debug, "debug", false, "enable printing debug messages")
	fs.Int64Var(&options.SegmentSize, "segment-size", convert.SegmentSize, "worker segment size in bytes")
	fs.IntVar(&options.BufferSize, "buffer-size", convert.BufferSize, "buffer size in bytes")
	fs.IntVar(&options.Workers, "workers", convert.Workers, "number of workers")
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
		return errors.New("target file is required")
	case 2:
		source = fs.Arg(0)
		target = fs.Arg(1)
	default:
		return errors.New("too many files were specified")
	}

	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()

	img, err := qcow2reader.Open(f)
	if err != nil {
		return err
	}
	defer img.Close()

	t, err := os.Create(target)
	if err != nil {
		return err
	}
	defer t.Close()

	if err := t.Truncate(img.Size()); err != nil {
		return err
	}

	c, err := convert.New(options)
	if err != nil {
		return err
	}
	if err := c.Convert(t, img, img.Size(), nil); err != nil {
		return err
	}

	if err := t.Sync(); err != nil {
		return err
	}

	return t.Close()
}
