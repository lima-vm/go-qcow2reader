# go-qcow2reader

[![Go Reference](https://pkg.go.dev/badge/github.com/lima-vm/go-qcow2reader.svg)](https://pkg.go.dev/github.com/lima-vm/go-qcow2reader)

go-qcow2reader provides [`io.ReaderAt`](https://pkg.go.dev/io#ReaderAt) for [qcow2](https://gitlab.com/qemu-project/qemu/-/blob/v8.0.0/docs/interop/qcow2.txt) images.

Use [`io.NewSectionReader`](https://pkg.go.dev/io#NewSectionReader) to wrap [`io.ReaderAt`](https://pkg.go.dev/io#ReaderAt) into [`io.Reader`](https://pkg.go.dev/io#Reader):
```go
f, _ := os.Open("a.qcow2")
defer f.Close()
img, _ := qcow2reader.Open(f)
r := io.NewSectionReader(img, 0, img.Size()))
```

## Converting images

The [`convert`](https://pkg.go.dev/github.com/lima-vm/go-qcow2reader/convert) package copies an image to an [`io.WriterAt`](https://pkg.go.dev/io#WriterAt),
skipping unallocated and zero extents. The target can be a raw file, or a
[`vhdx.Writer`](https://pkg.go.dev/github.com/lima-vm/go-qcow2reader/image/vhdx#Writer) to create a dynamic
[VHDX](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-vhdx/83e061f8-f6e2-4de1-91bd-5d518a43d477) image:
```go
f, _ := os.Create("a.vhdx")
defer f.Close()
w, _ := vhdx.NewWriter(f, img.Size(), vhdx.WriterOptions{})
_ = convert.Convert(w, img, convert.Options{})
_ = w.Close()
```

## Limitations

The following features are not supported yet:
- [AES](https://gitlab.com/qemu-project/qemu/-/blob/v8.0.0/docs/interop/qcow2.txt#L411-L421)
- [LUKS](https://gitlab.com/qemu-project/qemu/-/blob/v8.0.0/docs/interop/qcow2.txt#L423-L429)
- [External data](https://gitlab.com/qemu-project/qemu/-/blob/v8.0.0/docs/interop/qcow2.txt#L106-L116)

The following features are experimentally supported:
- [Extended L2 Entries](https://gitlab.com/qemu-project/qemu/-/blob/v8.0.0/docs/interop/qcow2.txt#L122-L126)
