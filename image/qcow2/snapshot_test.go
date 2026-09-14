package qcow2

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	imagepkg "github.com/lima-vm/go-qcow2reader/image"
)

func TestReadSnapshots(t *testing.T) {
	var table bytes.Buffer
	writeSnapshotTableEntry(t, &table, snapshotTableEntry{
		L1TableOffset: 65536,
		L1Size:        1,
		DateSec:       1234,
		DateNsec:      5678,
		VMClockNsec:   12_345_678_901,
		VMStateSize:   42,
	}, nil, "1", "first")

	extra := make([]byte, 24)
	binary.BigEndian.PutUint64(extra[0:8], 1<<32+42)
	binary.BigEndian.PutUint64(extra[8:16], 8<<30)
	binary.BigEndian.PutUint64(extra[16:24], 99)
	writeSnapshotTableEntry(t, &table, snapshotTableEntry{
		L1TableOffset: 131072,
		L1Size:        2,
		DateSec:       2345,
		DateNsec:      6789,
		VMClockNsec:   98_765_432_109,
		VMStateSize:   1, // Superseded by the 64-bit field in extra data.
	}, extra, "snapshot-id", "second snapshot")

	const offset = 4096
	image := append(make([]byte, offset), table.Bytes()...)
	snapshots, err := readSnapshots(bytes.NewReader(image), offset, 2)
	if err != nil {
		t.Fatal(err)
	}
	createdAt1 := time.Unix(1234, 5678).UTC()
	createdAt2 := time.Unix(2345, 6789).UTC()
	want := []Snapshot{
		{
			Snapshot: imagepkg.Snapshot{
				ID: "1", Name: "first",
				CreatedAt: &createdAt1,
			},
			VMStateSize: 42,
			VMClock:     12_345_678_901,
		},
		{
			Snapshot: imagepkg.Snapshot{
				ID: "snapshot-id", Name: "second snapshot",
				CreatedAt: &createdAt2,
			},
			VMStateSize: 1<<32 + 42,
			VMClock:     98_765_432_109,
		},
	}
	if !reflect.DeepEqual(snapshots, want) {
		t.Fatalf("expected %#v, got %#v", want, snapshots)
	}
}

func TestReadSnapshotsIgnoresQCOW2SpecificICount(t *testing.T) {
	var table bytes.Buffer
	extra := make([]byte, 24)
	binary.BigEndian.PutUint64(extra[16:24], math.MaxUint64)
	writeSnapshotTableEntry(t, &table, snapshotTableEntry{}, extra, "id", "name")

	// Offset zero is deliberately invalid, so prefix the table with one byte.
	image := append([]byte{0}, table.Bytes()...)
	snapshots, err := readSnapshots(bytes.NewReader(image), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %d", len(snapshots))
	}
}

func TestReadSnapshotsErrors(t *testing.T) {
	tests := []struct {
		name    string
		offset  uint64
		entries uint32
		want    string
	}{
		{name: "zero offset", offset: 0, entries: 1, want: "invalid snapshot table offset"},
		{name: "too many", offset: 1, entries: maxSnapshots + 1, want: "too many snapshots"},
		{name: "truncated", offset: 1, entries: 1, want: "failed to read snapshot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readSnapshots(bytes.NewReader(nil), tt.offset, tt.entries)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func writeSnapshotTableEntry(t *testing.T, buf *bytes.Buffer, ent snapshotTableEntry, extra []byte, id, name string) {
	t.Helper()
	ent.ExtraDataSize = uint32(len(extra))
	ent.IDSize = uint16(len(id))
	ent.NameSize = uint16(len(name))
	if err := binary.Write(buf, binary.BigEndian, &ent); err != nil {
		t.Fatal(err)
	}
	buf.Write(extra)
	buf.WriteString(id)
	buf.WriteString(name)
	for buf.Len()%8 != 0 {
		buf.WriteByte(0)
	}
}
