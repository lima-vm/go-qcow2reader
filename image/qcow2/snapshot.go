package qcow2

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/lima-vm/go-qcow2reader/align"
	"github.com/lima-vm/go-qcow2reader/image"
)

var _ image.SnapshotReader = (*Qcow2)(nil)

// Snapshot wraps [image.Snapshot] with qcow2-specific fields.
type Snapshot struct {
	image.Snapshot
	// VMStateSize is the size of the VM state in bytes. 0 if no VM state is saved.
	VMStateSize uint64 `json:"vm_state_size"`
	// VMClock is the time that the guest was running until the snapshot was taken.
	VMClock time.Duration `json:"vm_clock"`
}

// maxSnapshots is the maximum number of snapshots.
// Corresponds to QCOW_MAX_SNAPSHOTS in QEMU.
const maxSnapshots = 65536

// maxSnapshotExtraData is the maximum size of extra data in a snapshot table
// entry. Corresponds to QCOW_MAX_SNAPSHOT_EXTRA_DATA in QEMU.
const maxSnapshotExtraData = 1024

// snapshotTableEntry is the static part of an entry of the snapshot table.
// The entry is followed by the extra data, the unique ID string, the name,
// and padding to the next multiple of 8 bytes.
type snapshotTableEntry struct {
	// L1TableOffset is the offset into the image file at which the L1 table
	// for the snapshot starts.
	L1TableOffset uint64
	// L1Size is the number of entries in the L1 table of the snapshot.
	L1Size uint32
	// IDSize is the length of the unique ID string describing the snapshot.
	IDSize uint16
	// NameSize is the length of the name of the snapshot.
	NameSize uint16
	// DateSec is the time at which the snapshot was taken in seconds since the Epoch.
	DateSec uint32
	// DateNsec is the subsecond part of the time at which the snapshot was taken.
	DateNsec uint32
	// VMClockNsec is the time that the guest was running until the snapshot
	// was taken in nanoseconds.
	VMClockNsec uint64
	// VMStateSize is the size of the VM state in bytes. 0 if no VM state is saved.
	// Superseded by the 64-bit field in the extra data, if present.
	VMStateSize uint32
	// ExtraDataSize is the size of extra data in the table entry.
	ExtraDataSize uint32
}

// readSnapshots reads the snapshot table.
func readSnapshots(ra io.ReaderAt, offset uint64, entries uint32) ([]Snapshot, error) {
	if offset == 0 {
		return nil, errors.New("invalid snapshot table offset: 0")
	}
	if entries > maxSnapshots {
		return nil, fmt.Errorf("too many snapshots (%d entries > %d entries)", entries, maxSnapshots)
	}
	r := io.NewSectionReader(ra, int64(offset), -1)
	snapshots := make([]Snapshot, entries)
	for i := range snapshots {
		var ent snapshotTableEntry
		if err := binary.Read(r, binary.BigEndian, &ent); err != nil {
			return nil, fmt.Errorf("failed to read snapshot %d: %w", i, err)
		}
		if ent.ExtraDataSize > maxSnapshotExtraData {
			return nil, fmt.Errorf("failed to read snapshot %d: too much extra data (%d bytes > %d bytes)",
				i, ent.ExtraDataSize, maxSnapshotExtraData)
		}
		buf := make([]byte, int(ent.ExtraDataSize)+int(ent.IDSize)+int(ent.NameSize))
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, fmt.Errorf("failed to read snapshot %d: %w", i, err)
		}
		extra := buf[:ent.ExtraDataSize]
		id := buf[ent.ExtraDataSize : ent.ExtraDataSize+uint32(ent.IDSize)]
		name := buf[ent.ExtraDataSize+uint32(ent.IDSize):]
		createdAt := time.Unix(int64(ent.DateSec), int64(ent.DateNsec)).UTC()
		sn := Snapshot{
			Snapshot: image.Snapshot{
				ID:        string(id),
				Name:      string(name),
				CreatedAt: &createdAt,
			},
			VMStateSize: uint64(ent.VMStateSize),
			VMClock:     time.Duration(ent.VMClockNsec),
		}
		if len(extra) >= 8 {
			sn.VMStateSize = binary.BigEndian.Uint64(extra[0:8])
		}
		// extra[8:16] (virtual disk size of the snapshot) and extra[16:24]
		// (icount value) are not exposed in [Snapshot].
		snapshots[i] = sn

		// The snapshot table entry is padded to the next multiple of 8 bytes.
		// The static part is already a multiple of 8 bytes, so only the
		// variable part matters.
		if pad := align.Up(len(buf), 8) - len(buf); pad > 0 {
			if _, err := r.Seek(int64(pad), io.SeekCurrent); err != nil {
				return nil, fmt.Errorf("failed to read snapshot %d: %w", i, err)
			}
		}
	}
	return snapshots, nil
}
