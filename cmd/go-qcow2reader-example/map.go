package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/log"
)

func cmdMap(args []string) error {
	var (
		// Required
		filename string

		// Options
		debug bool
	)

	fs := flag.NewFlagSet("map", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s map [OPTIONS...] FILE\n", os.Args[0])
		flag.PrintDefaults()
	}
	fs.BoolVar(&debug, "debug", false, "enable printing debug messages")
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

	writer := bufio.NewWriter(os.Stdout)
	encoder := json.NewEncoder(writer)

	var start int64
	end := img.Size()
	for start < end {
		extent, err := img.Extent(start, end-start)
		if err != nil {
			return err
		}
		encoder.Encode(extent)
		start += extent.Length
	}
	return writer.Flush()
}
