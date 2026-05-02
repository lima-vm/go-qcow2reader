package convert

import (
	"fmt"
	"testing"
	"unsafe"
)

func TestValidateAlignment(t *testing.T) {
	for _, alignment := range []int{512, 4096} {
		t.Run(fmt.Sprintf("%d", alignment), func(t *testing.T) {
			opts := Options{Alignment: alignment}
			if err := opts.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateAlignmentInvalid(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "alignment not power of 2",
			opts: Options{Alignment: 3000},
		},
		{
			name: "buffer size not aligned",
			opts: Options{Alignment: 4096, BufferSize: 5000, SegmentSize: 5000},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.opts.Validate(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAllocateBuffer(t *testing.T) {
	opts := Options{}
	if err := opts.Validate(); err != nil {
		t.Fatal(err)
	}
	buf := allocateBuffer(&opts)
	if len(buf) != opts.BufferSize {
		t.Fatalf("expected len %d, got %d", opts.BufferSize, len(buf))
	}
}

func TestAllocateBufferAligned(t *testing.T) {
	for _, alignment := range []int{512, 4096} {
		t.Run(fmt.Sprintf("%d", alignment), func(t *testing.T) {
			opts := Options{
				Alignment: alignment,
			}
			if err := opts.Validate(); err != nil {
				t.Fatal(err)
			}
			buf := allocateBuffer(&opts)
			if len(buf) != opts.BufferSize {
				t.Fatalf("expected len %d, got %d", opts.BufferSize, len(buf))
			}
			addr := uintptr(unsafe.Pointer(&buf[0]))
			if addr%uintptr(alignment) != 0 {
				t.Fatalf("buffer address %x not aligned to %d", addr, alignment)
			}
		})
	}
}
