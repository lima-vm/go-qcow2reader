package vhdx

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"unicode/utf16"
)

// This file implements a writer for dynamic VHDX images, following
// [MS-VHDX]: Virtual Hard Disk v2 (VHDX) File Format, revision 8.0.
// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-vhdx/83e061f8-f6e2-4de1-91bd-5d518a43d477
//
// The file layout produced by Writer is:
//
//	0        file type identifier ("vhdxfile")
//	64 KiB   header 1
//	128 KiB  header 2
//	192 KiB  region table 1
//	256 KiB  region table 2
//	1 MiB    log (1 MiB, empty)
//	2 MiB    metadata region (1 MiB)
//	3 MiB    BAT region (multiple of 1 MiB)
//	...      payload blocks, allocated in the order they are first written
//
// All integers are little-endian. GUIDs are stored in the Windows binary form
// (mixed-endian).

const (
	kiB = int64(1) << 10
	miB = int64(1) << 20
	tiB = int64(1) << 40

	// MinBlockSize is the minimum payload block size (section 2.6.2.1).
	MinBlockSize = 1 * miB
	// MaxBlockSize is the maximum payload block size (section 2.6.2.1).
	MaxBlockSize = 256 * miB
	// MaxVirtualDiskSize is the maximum virtual disk size (section 2.6.2.2).
	MaxVirtualDiskSize = 64 * tiB

	// LogicalSectorSize is the virtual disk sector size written by Writer.
	LogicalSectorSize = 512
	// PhysicalSectorSize is the virtual disk physical sector size written by
	// Writer.
	PhysicalSectorSize = 512

	// Header section (section 2.2).
	header1Offset      = 64 * kiB
	header2Offset      = 128 * kiB
	regionTable1Offset = 192 * kiB
	regionTable2Offset = 256 * kiB
	headerSectionSize  = 1 * miB
	headerSize         = 4 * kiB
	regionTableSize    = 64 * kiB

	// Log (section 2.3). The log is left empty (LogGuid is zero).
	logOffset = headerSectionSize
	logSize   = 1 * miB

	// Metadata region (section 2.6).
	metadataOffset      = logOffset + logSize
	metadataSize        = 1 * miB
	metadataItemsOffset = 64 * kiB // relative to the metadata region

	// BAT region (section 2.5).
	batOffset = metadataOffset + metadataSize

	// BAT entry payload block states (section 2.5.1.1). The other states
	// (NOT_PRESENT=0, UNDEFINED=1, UNMAPPED=3, PARTIALLY_PRESENT=7) are not
	// written by Writer.
	payloadBlockZero         = 2
	payloadBlockFullyPresent = 6

	batEntrySize = 8

	// The number of sectors described by one sector bitmap block (section 2.4).
	sectorsPerBitmapBlock = int64(1) << 23

	// Metadata table entry flags (section 2.6.1.2). IsUser (1 << 0) is not
	// used.
	metadataIsVirtualDisk = 1 << 1
	metadataIsRequired    = 1 << 2

	creator = "go-qcow2reader"
)

var (
	fileTypeSignature      = []byte("vhdxfile")
	headerSignature        = []byte("head")
	regionTableSignature   = []byte("regi")
	metadataTableSignature = []byte("metadata")

	// Known regions (section 2.2.3.2).
	batRegionGUID      = mustGUID("2DC27766-F623-4200-9D64-115E9BFD4A08")
	metadataRegionGUID = mustGUID("8B7CA206-4790-4B9A-B8FE-575F050F886E")

	// Known metadata items (section 2.6.2).
	fileParametersGUID     = mustGUID("CAA16737-FA36-4D43-B3B6-33F0AA44E76B")
	virtualDiskSizeGUID    = mustGUID("2FA54224-CD1B-4876-B211-5DBED83BF4B8")
	virtualDiskIDGUID      = mustGUID("BECA12AB-B2E6-4523-93EF-C309E000C746")
	logicalSectorSizeGUID  = mustGUID("8141BF1D-A96F-4709-BA47-F233A8FAAB5F")
	physicalSectorSizeGUID = mustGUID("CDA348C7-445D-4471-9CC9-E9885251C556")

	crc32cTable = crc32.MakeTable(crc32.Castagnoli)
)

// guid is a GUID in the Windows binary form: the first three fields are
// little-endian, the remaining 8 bytes are stored as is.
type guid [16]byte

// mustGUID parses the textual form "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE" into
// the binary form.
func mustGUID(s string) guid {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		panic("invalid GUID: " + s)
	}
	raw, err := hex.DecodeString(s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:36])
	if err != nil {
		panic("invalid GUID: " + s)
	}
	var g guid
	g[0], g[1], g[2], g[3] = raw[3], raw[2], raw[1], raw[0]
	g[4], g[5] = raw[5], raw[4]
	g[6], g[7] = raw[7], raw[6]
	copy(g[8:], raw[8:])
	return g
}

// newRandomGUID returns a random (version 4) GUID.
func newRandomGUID() (guid, error) {
	var g guid
	if _, err := rand.Read(g[:]); err != nil {
		return g, err
	}
	// In the binary form, the version nibble is the high nibble of byte 7 and
	// the variant bits are in byte 8.
	g[7] = (g[7] & 0x0f) | 0x40
	g[8] = (g[8] & 0x3f) | 0x80
	return g, nil
}

func crc32c(b []byte) uint32 {
	return crc32.Checksum(b, crc32cTable)
}

// WriterOptions specifies options for [NewWriter].
type WriterOptions struct {
	// BlockSize is the payload block size in bytes. Must be a power of two
	// between MinBlockSize (1 MiB) and MaxBlockSize (256 MiB). If zero, use
	// [DefaultBlockSize].
	//
	// Blocks that are never written are not allocated in the file, so a smaller
	// block size yields a smaller file for sparse disks, at the cost of a larger
	// block allocation table, and more allocations when the virtual disk is
	// written by a guest later.
	BlockSize int64
}

// DefaultBlockSize returns the default payload block size for a virtual disk
// of the specified size: the smallest block size (1 MiB) for disks up to 1
// TiB, doubling with every doubling of the disk size beyond that, so that the
// block allocation table has at most 1 Mi entries (8 MiB).
//
// The default favors a small image over the larger block sizes used by other
// tools (typically 8 MiB to 32 MiB), since the unwritten part of a partially
// written block is allocated by file systems that do not support sparse
// files, or that fill small holes.
func DefaultBlockSize(size int64) int64 {
	blockSize := MinBlockSize
	for blockSize < MaxBlockSize && size > blockSize<<20 {
		blockSize <<= 1
	}
	return blockSize
}

// Validate validates options and sets default values for a virtual disk of
// the specified size.
func (o *WriterOptions) Validate(size int64) error {
	if o.BlockSize == 0 {
		o.BlockSize = DefaultBlockSize(size)
	}
	if o.BlockSize < MinBlockSize || o.BlockSize > MaxBlockSize {
		return fmt.Errorf("block size %d must be between %d and %d", o.BlockSize, MinBlockSize, MaxBlockSize)
	}
	if o.BlockSize&(o.BlockSize-1) != 0 {
		return fmt.Errorf("block size %d must be a power of two", o.BlockSize)
	}
	return nil
}

// Writer writes a dynamic VHDX image to an [io.WriterAt].
//
// Writer implements [io.WriterAt] for the virtual disk: writing at offset off
// stores the data at virtual disk offset off, allocating payload blocks as
// needed. Ranges that are never written read as zeros, and payload blocks
// that would contain only zeros are not allocated, so unallocated or zero
// extents of a source image are converted to unallocated blocks in the VHDX
// image. This makes Writer a suitable target for
// [github.com/lima-vm/go-qcow2reader/convert.Convert].
//
// The target must be a new empty file (or a file full of zeroes): blocks are
// allocated at increasing offsets, and bytes within an allocated block that
// are never written must read as zeros. If the target implements Truncate
// (as [os.File] does), the file is extended by truncation when blocks are
// allocated, so that the unwritten parts of the file remain holes on file
// systems supporting sparse files, including those that do not create holes
// for writes beyond the end of the file (e.g. APFS).
//
// Writer is safe for concurrent use by multiple goroutines. Close must be
// called after all writes to write the block allocation table and finalize
// the image; the image is not recognizable as a VHDX file before Close.
// Writer does not close the underlying [io.WriterAt].
type Writer struct {
	w io.WriterAt
	// truncate extends the file to the specified size, or nil if w does not
	// support truncation.
	truncate  func(size int64) error
	size      int64 // virtual disk size
	blockSize int64
	// blocks holds the file offset of each payload block, or 0 if the block is
	// not allocated.
	blocks []int64
	// chunkRatio is the number of payload blocks per sector bitmap block.
	chunkRatio int64
	// batLength is the length of the BAT region in the file.
	batLength int64
	// next is the file offset of the next payload block to allocate.
	next int64
	// end is the end offset of the furthest payload write, used to extend the
	// file to the end of the last allocated block in Close.
	end int64

	fileWriteGUID guid
	dataWriteGUID guid
	virtualDiskID guid

	mu     sync.Mutex
	closed bool
}

// NewWriter creates a dynamic VHDX image with the specified virtual disk size
// in bytes, writing to w. The size must be a positive multiple of
// LogicalSectorSize (512) and at most MaxVirtualDiskSize (64 TiB).
//
// The static structures (headers, region tables, and metadata) are written
// immediately; the block allocation table and the file type identifier are
// written by Close.
func NewWriter(w io.WriterAt, size int64, opts WriterOptions) (*Writer, error) {
	if size <= 0 {
		return nil, fmt.Errorf("virtual disk size %d must be positive", size)
	}
	if size > MaxVirtualDiskSize {
		return nil, fmt.Errorf("virtual disk size %d exceeds the maximum %d", size, MaxVirtualDiskSize)
	}
	if size%LogicalSectorSize != 0 {
		return nil, fmt.Errorf("virtual disk size %d is not a multiple of the sector size %d", size, LogicalSectorSize)
	}
	if err := opts.Validate(size); err != nil {
		return nil, err
	}

	blockSize := opts.BlockSize
	payloadBlocks := (size + blockSize - 1) / blockSize

	// The BAT interleaves one sector bitmap block entry after every chunkRatio
	// payload block entries. For a dynamic image, the last entry must locate the
	// last payload block (section 2.5).
	chunkRatio := sectorsPerBitmapBlock * LogicalSectorSize / blockSize
	batEntries := payloadBlocks + (payloadBlocks-1)/chunkRatio
	batLength := (batEntries*batEntrySize + miB - 1) / miB * miB

	wr := &Writer{
		w:          w,
		truncate:   truncateFunc(w),
		size:       size,
		blockSize:  blockSize,
		blocks:     make([]int64, payloadBlocks),
		chunkRatio: chunkRatio,
		batLength:  batLength,
		next:       batOffset + batLength,
	}

	// Extend the file to the end of the BAT region, so that the unwritten parts
	// of the header section, the log, the metadata region and the BAT are holes.
	if wr.truncate != nil {
		if err := wr.truncate(wr.next); err != nil {
			return nil, err
		}
	}

	var err error
	if wr.fileWriteGUID, err = newRandomGUID(); err != nil {
		return nil, err
	}
	if wr.dataWriteGUID, err = newRandomGUID(); err != nil {
		return nil, err
	}
	if wr.virtualDiskID, err = newRandomGUID(); err != nil {
		return nil, err
	}

	if err := wr.writeAll(wr.header(1), header1Offset); err != nil {
		return nil, err
	}
	if err := wr.writeAll(wr.header(2), header2Offset); err != nil {
		return nil, err
	}
	regionTable := wr.regionTable()
	if err := wr.writeAll(regionTable, regionTable1Offset); err != nil {
		return nil, err
	}
	if err := wr.writeAll(regionTable, regionTable2Offset); err != nil {
		return nil, err
	}
	if err := wr.writeAll(wr.metadata(), metadataOffset); err != nil {
		return nil, err
	}
	return wr, nil
}

// Size returns the virtual disk size in bytes.
func (wr *Writer) Size() int64 {
	return wr.size
}

// BlockSize returns the payload block size in bytes.
func (wr *Writer) BlockSize() int64 {
	return wr.blockSize
}

// WriteAt writes len(p) bytes from p to the virtual disk at offset off. It
// returns an error if the range is outside the virtual disk.
func (wr *Writer) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off > wr.size || int64(len(p)) > wr.size-off {
		return 0, fmt.Errorf("write of %d bytes at offset %d is out of bounds (virtual disk size %d)", len(p), off, wr.size)
	}
	n := 0
	for len(p) > 0 {
		block := off / wr.blockSize
		blockOff := off % wr.blockSize
		chunk := min(int64(len(p)), wr.blockSize-blockOff)

		fileOff, err := wr.blockOffset(block, p[:chunk])
		if err != nil {
			return n, err
		}
		if fileOff != 0 {
			nw, err := wr.w.WriteAt(p[:chunk], fileOff+blockOff)
			n += nw
			if err != nil {
				return n, err
			}
			if int64(nw) != chunk {
				return n, io.ErrShortWrite
			}
			wr.mu.Lock()
			wr.end = max(wr.end, fileOff+blockOff+chunk)
			wr.mu.Unlock()
		} else {
			// The block is not allocated and the data is all zeros.
			n += int(chunk)
		}
		p = p[chunk:]
		off += chunk
	}
	return n, nil
}

// blockOffset returns the file offset of the payload block, allocating it if
// needed. Returns 0 if the block is not allocated and data is all zeros, in
// which case there is nothing to write.
func (wr *Writer) blockOffset(block int64, data []byte) (int64, error) {
	wr.mu.Lock()
	closed, fileOff := wr.closed, wr.blocks[block]
	wr.mu.Unlock()
	if closed {
		return 0, os.ErrClosed
	}
	// Scan the data without holding the mutex, then check again since another
	// goroutine may have allocated the block meanwhile.
	if fileOff != 0 || isZero(data) {
		return fileOff, nil
	}

	wr.mu.Lock()
	defer wr.mu.Unlock()
	if wr.closed {
		return 0, os.ErrClosed
	}
	if fileOff := wr.blocks[block]; fileOff != 0 {
		return fileOff, nil
	}
	// Extend the file with a hole for the new block, so that the parts of the
	// block that are never written are not allocated in the file system.
	if wr.truncate != nil {
		if err := wr.truncate(wr.next + wr.blockSize); err != nil {
			return 0, err
		}
	}
	fileOff = wr.next
	wr.next += wr.blockSize
	wr.blocks[block] = fileOff
	return fileOff, nil
}

// truncateFunc returns the Truncate method of w, or nil if w does not
// implement it.
func truncateFunc(w io.WriterAt) func(size int64) error {
	if t, ok := w.(interface{ Truncate(size int64) error }); ok {
		return t.Truncate
	}
	return nil
}

// Close writes the block allocation table and the file type identifier,
// finalizing the image. Close does not close the underlying [io.WriterAt].
// WriteAt and Close return [os.ErrClosed] after the writer was closed.
func (wr *Writer) Close() error {
	wr.mu.Lock()
	if wr.closed {
		wr.mu.Unlock()
		return os.ErrClosed
	}
	wr.closed = true
	wr.mu.Unlock()

	if err := wr.writeBAT(); err != nil {
		return err
	}

	// Make sure that the file extends to the end of the last allocated block, or
	// to the end of the BAT region if no block was allocated. If the file could
	// not be extended by truncation, and unless the last byte was written, it
	// must read as zero, so writing a single zero byte extends the file while
	// keeping it sparse.
	if wr.truncate == nil && wr.end < wr.next {
		if err := wr.writeAll([]byte{0}, wr.next-1); err != nil {
			return err
		}
	}

	// The file type identifier is written last, so that a file which was not
	// completely written is not recognized as a VHDX image.
	return wr.writeAll(wr.fileTypeIdentifier(), 0)
}

func (wr *Writer) writeAll(b []byte, off int64) error {
	n, err := wr.w.WriteAt(b, off)
	if err != nil {
		return err
	}
	if n != len(b) {
		return io.ErrShortWrite
	}
	return nil
}

// fileTypeIdentifier returns the file type identifier (section 2.2.1).
func (wr *Writer) fileTypeIdentifier() []byte {
	b := make([]byte, 8+512)
	copy(b, fileTypeSignature)
	// Creator is an optional UTF-16 string.
	for i, r := range utf16.Encode([]rune(creator)) {
		binary.LittleEndian.PutUint16(b[8+2*i:], r)
	}
	return b
}

// header returns a header (section 2.2.2) with the specified sequence number.
func (wr *Writer) header(sequenceNumber uint64) []byte {
	b := make([]byte, headerSize)
	copy(b[0:], headerSignature)
	// Checksum at 4.
	binary.LittleEndian.PutUint64(b[8:], sequenceNumber)
	copy(b[16:], wr.fileWriteGUID[:])
	copy(b[32:], wr.dataWriteGUID[:])
	// LogGuid at 48: zero, the log is empty.
	// LogVersion at 64: 0.
	binary.LittleEndian.PutUint16(b[66:], 1) // Version
	binary.LittleEndian.PutUint32(b[68:], uint32(logSize))
	binary.LittleEndian.PutUint64(b[72:], uint64(logOffset))
	binary.LittleEndian.PutUint32(b[4:], crc32c(b))
	return b
}

// regionTable returns the region table (section 2.2.3).
func (wr *Writer) regionTable() []byte {
	b := make([]byte, regionTableSize)
	copy(b[0:], regionTableSignature)
	// Checksum at 4.
	binary.LittleEndian.PutUint32(b[8:], 2) // EntryCount
	// Reserved at 12.
	regionTableEntry(b[16:], batRegionGUID, batOffset, wr.batLength)
	regionTableEntry(b[48:], metadataRegionGUID, metadataOffset, metadataSize)
	binary.LittleEndian.PutUint32(b[4:], crc32c(b))
	return b
}

// regionTableEntry encodes a required region table entry (section 2.2.3.2).
func regionTableEntry(b []byte, id guid, fileOffset, length int64) {
	copy(b[0:], id[:])
	binary.LittleEndian.PutUint64(b[16:], uint64(fileOffset))
	binary.LittleEndian.PutUint32(b[24:], uint32(length))
	binary.LittleEndian.PutUint32(b[28:], 1) // Required
}

// metadata returns the beginning of the metadata region: the metadata table
// (section 2.6.1) followed by the known metadata items (section 2.6.2) at
// metadataItemsOffset. The rest of the region is unused.
func (wr *Writer) metadata() []byte {
	items := []struct {
		id     guid
		flags  uint32
		length int
		encode func([]byte)
	}{
		{fileParametersGUID, metadataIsRequired, 8, func(b []byte) {
			binary.LittleEndian.PutUint32(b[0:], uint32(wr.blockSize))
			// Flags at 4: LeaveBlockAllocated=0, HasParent=0.
		}},
		{virtualDiskSizeGUID, metadataIsVirtualDisk | metadataIsRequired, 8, func(b []byte) {
			binary.LittleEndian.PutUint64(b[0:], uint64(wr.size))
		}},
		{virtualDiskIDGUID, metadataIsVirtualDisk | metadataIsRequired, 16, func(b []byte) {
			copy(b[0:], wr.virtualDiskID[:])
		}},
		{logicalSectorSizeGUID, metadataIsVirtualDisk | metadataIsRequired, 4, func(b []byte) {
			binary.LittleEndian.PutUint32(b[0:], LogicalSectorSize)
		}},
		{physicalSectorSizeGUID, metadataIsVirtualDisk | metadataIsRequired, 4, func(b []byte) {
			binary.LittleEndian.PutUint32(b[0:], PhysicalSectorSize)
		}},
	}

	b := make([]byte, metadataItemsOffset+40)
	copy(b[0:], metadataTableSignature)
	// Reserved at 8.
	binary.LittleEndian.PutUint16(b[10:], uint16(len(items))) // EntryCount
	// Reserved2 at 12.

	itemOffset := int(metadataItemsOffset)
	for i, item := range items {
		entry := b[32+32*i:]
		copy(entry[0:], item.id[:])
		binary.LittleEndian.PutUint32(entry[16:], uint32(itemOffset))
		binary.LittleEndian.PutUint32(entry[20:], uint32(item.length))
		binary.LittleEndian.PutUint32(entry[24:], item.flags)
		// Reserved2 at 28.
		item.encode(b[itemOffset : itemOffset+item.length])
		itemOffset += item.length
	}
	return b
}

// writeBAT writes the block allocation table (section 2.5).
func (wr *Writer) writeBAT() error {
	// Write the BAT in bounded chunks, since it can be large for a huge disk
	// with small blocks.
	buf := make([]byte, 0, miB)
	off := int64(batOffset)
	flush := func() error {
		if err := wr.writeAll(buf, off); err != nil {
			return err
		}
		off += int64(len(buf))
		buf = buf[:0]
		return nil
	}
	emit := func(entry uint64) error {
		buf = binary.LittleEndian.AppendUint64(buf, entry)
		if len(buf) == cap(buf) {
			return flush()
		}
		return nil
	}
	for i, fileOff := range wr.blocks {
		// The sector bitmap block entry following each chunk is left as zero
		// (SB_BLOCK_NOT_PRESENT), which is the expected state for a dynamic image.
		if i > 0 && int64(i)%wr.chunkRatio == 0 {
			if err := emit(0); err != nil {
				return err
			}
		}
		entry := uint64(payloadBlockZero)
		if fileOff != 0 {
			entry = payloadBlockFullyPresent | uint64(fileOff/miB)<<20
		}
		if err := emit(entry); err != nil {
			return err
		}
	}
	return flush()
}

var zeros = make([]byte, 64*kiB)

// isZero returns true if b contains only zeros.
func isZero(b []byte) bool {
	for len(b) > 0 {
		n := min(len(b), len(zeros))
		if !bytes.Equal(b[:n], zeros[:n]) {
			return false
		}
		b = b[n:]
	}
	return true
}

var (
	_ io.WriterAt = (*Writer)(nil)
	_ io.Closer   = (*Writer)(nil)
)
