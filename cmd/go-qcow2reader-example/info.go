package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lima-vm/go-qcow2reader"
	"github.com/lima-vm/go-qcow2reader/image"
)

func cmdInfo(args []string) error {
	var (
		// Required
		filename string

		// Options
		debug bool
	)

	fs := flag.NewFlagSet("info", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s info [OPTIONS...] FILE\n", os.Args[0])
		fs.PrintDefaults()
	}
	fs.BoolVar(&debug, "debug", false, "enable printing debug messages")
	if err := fs.Parse(args); err != nil {
		return err
	}

	switch len(fs.Args()) {
	case 0:
		return errors.New("no file was specified")
	case 1:
		filename = fs.Arg(0)
	default:
		return errors.New("too many files specified")
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

	imgInfo := image.NewImageInfo(img)
	j, err := json.MarshalIndent(imgInfo, "", "    ")
	if err != nil {
		return err
	}
	if _, err = fmt.Println(string(j)); err != nil {
		return err
	}
	if err = img.Readable(); err != nil {
		logWarn(err.Error())
	}

	return nil
}
