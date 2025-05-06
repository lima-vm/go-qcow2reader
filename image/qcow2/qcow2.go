package qcow2

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lima-vm/go-qcow2reader/align"
	"github.com/lima-vm/go-qcow2reader/image"
	"github.com/lima-vm/go-qcow2reader/log"
	"github.com/lima-vm/go-qcow2reader/lru"
)

const Type = "qcow2"

// Magic is the qcow2 magic string.
const Magic = "QFI\xfb"

// MagicType wraps magic bytes.
type MagicType [4]byte

// String implements [fmt.Stringer].
func (x MagicType) String() string {
	return string(x[:])
}

// MarshalText implements [encoding.TextMarshaler].
func (x MagicType) MarshalText() ([]byte, error) {
	return x[:], nil
}

type CryptMethod uint32

const (
	CryptMethodNone = CryptMethod(0)
	CryptMethodAES  = CryptMethod(1)
	CryptMethodLUKS = CryptMethod(2)
)

func (x CryptMethod) String() string {
	switch x {
	case CryptMethodNone:
		return ""
	case CryptMethodAES:
		return "aes"
	case CryptMethodLUKS:
		return "luks"
	default:
		return fmt.Sprintf("unknown-%d", int(x))
	}
}

func (x CryptMethod) MarshalText() ([]byte, error) {
	return []byte(x.String()), nil
}

type HeaderFieldsV2 struct {
	Magic                 MagicType   `json:"magic"`
	Version               uint32      `json:"version"`             // 2 or 3
	BackingFileOffset     uint64      `json:"backing_file_offset"` // offset of file name (not null terminated)
	BackingFileSize       uint32      `json:"backing_file_size"`   // length of file name (<= 1023)
	ClusterBits           uint32      `json:"cluster_bits"`
	Size                  uint64      `json:"size"` // Virtual disk size in bytes
	CryptMethod           CryptMethod `json:"crypt_method"`
	L1Size                uint32      `json:"l1_size"`         // Number of entries
	L1TableOffset         uint64      `json:"l1_table_offset"` // Offset into the image file
	RefcountTableOffset   uint64      `json:"refcount_table_offset"`
	RefcountTableClusters uint32      `json:"refcount_table_clusters"`
	NbSnapshots           uint32      `json:"nb_snapshots"` // Number of snapshots
	SnapshotsOffset       uint64      `json:"snapshots_offset"`
}

type IncompatibleFeatures uint64

const (
	IncompatibleFeaturesDirtyBit             = 0
	IncompatibleFeaturesCorruptBit           = 1
	IncompatibleFeaturesExternalDataFileBit  = 2
	IncompatibleFeaturesCompressionTypeBit   = 3
	IncompatibleFeaturesExtendedL2EntriesBit = 4
)

var IncompatibleFeaturesNames = []string{
	"dirty bit",           // 0
	"corrupt bit",         // 1
	"external data file",  // 2
	"compression type",    // 3
	"extended L2 entries", // 4
}

func activeFeaturesNames(features uint64, names []string) []string {
	var res []string
	for i := 0; i < 64; i++ {
		if (features>>i)&0b1 == 0b1 {
			name := fmt.Sprintf("unknown-%d", i)
			if i < len(names) {
				name = names[i]
			}
			res = append(res, name)
		}
	}
	return res
}

type Features struct {
	Raw   uint64   `json:"raw"`
	Names []string `json:"names"`
}

func newFeatures(x uint64, names []string) *Features {
	if x == 0 {
		return nil
	}
	return &Features{Raw: x, Names: activeFeaturesNames(x, names)}
}

func (x IncompatibleFeatures) MarshalJSON() ([]byte, error) {
	return json.Marshal(newFeatures(uint64(x), IncompatibleFeaturesNames))
}

type CompatibleFeatures uint64

const (
	CompatibleFeaturesLazyRefcountsBit = 0
)

var CompatibleFeaturesNames = []string{
	"lazy refcounts", // 0
}

func (x CompatibleFeatures) MarshalJSON() ([]byte, error) {
	return json.Marshal(newFeatures(uint64(x), CompatibleFeaturesNames))
}

type AutoclearFeatures uint64

const (
	AutoclearFeaturesBitmapsExtensionBit = 0
	AutoclearFeaturesRawExternalBit      = 1
)

var AutoclearFeaturesNames = []string{
	"bitmaps",           // 0
	"raw external data", // 1
}

func (x AutoclearFeatures) MarshalJSON() ([]byte, error) {
	return json.Marshal(newFeatures(uint64(x), AutoclearFeaturesNames))
}

type HeaderFieldsV3 struct {
	IncompatibleFeatures IncompatibleFeatures `json:"incompatible_features"`
	CompatibleFeatures   CompatibleFeatures   `json:"compatible_features"`
	AutoclearFeatures    AutoclearFeatures    `json:"autoclear_features"`
	RefcountOrder        uint32               `json:"refcount_order"`
	HeaderLength         uint32               `json:"header_length"`
}

type CompressionType uint8

const (
	// CompressionTypeZlib is a misnomer. It is actually deflate without zlib header.
	CompressionTypeZlib = CompressionType(0)
	CompressionTypeZstd = CompressionType(1)
)

func (x CompressionType) String() string {
	switch x {
	case CompressionTypeZlib:
		return "zlib" // misnomer; actually deflate without zlib header
	case CompressionTypeZstd:
		return "zstd"
	default:
		return fmt.Sprintf("unknown-%d", int(x))
	}
}

func (x CompressionType) MarshalText() ([]byte, error) {
	return []byte(x.String()), nil
}

type Decompressor func(r io.Reader) (io.ReadCloser, error)

var decompressors = map[CompressionType]Decompressor{
	// no zlib header
	CompressionTypeZlib: func(r io.Reader) (io.ReadCloser, error) {
		return flate.NewReader(r), nil
	},
}

// SetDecompressor sets a custom decompressor.
// By default, [flate.NewReader] is registered for [CompressionTypeZlib].
// No decompressor is registered by default for [CompressionTypeZstd].
func SetDecompressor(t CompressionType, d Decompressor) {
	decompressors[t] = d
}

type HeaderFieldsAdditional struct {
	CompressionType CompressionType `json:"compression_type"`
	// Pad is exposed to avoid `panic: reflect: reflect.Value.SetUint using value obtained using unexported field` during [binary.Read].
	Pad [7]byte `json:"-"`
}

type Header struct {
	HeaderFieldsV2
	*HeaderFieldsV3
	*HeaderFieldsAdditional
}

func (header *Header) Length() int {
	if header.HeaderFieldsV3 != nil {
		return int(header.HeaderLength)
	}
	return 72
}

type HeaderExtensionType uint32

const (
	HeaderExtensionTypeEnd                             = HeaderExtensionType(0x00000000)
	HeaderExtensionTypeBackingFileFormatNameString     = HeaderExtensionType(0xe2792aca)
	HeaderExtensionTypeFeatureNameTable                = HeaderExtensionType(0x6803f857)
	HeaderExtensionTypeBitmapsExtension                = HeaderExtensionType(0x23852875)
	HeaderExtensionTypeFullDiskEncryptionHeaderPointer = HeaderExtensionType(0x0537be77)
	HeaderExtensionTypeExternalDataFileNameString      = HeaderExtensionType(0x44415441)
)

func (x HeaderExtensionType) String() string {
	switch x {
	case HeaderExtensionTypeEnd:
		return "End of the header extension area"
	case HeaderExtensionTypeBackingFileFormatNameString:
		return "Backing file format name string"
	case HeaderExtensionTypeFeatureNameTable:
		return "Feature name table"
	case HeaderExtensionTypeBitmapsExtension:
		return "Bitmaps extension"
	case HeaderExtensionTypeFullDiskEncryptionHeaderPointer:
		return "Full disk encryption header pointer"
	case HeaderExtensionTypeExternalDataFileNameString:
		return "External data file name string"
	default:
		return fmt.Sprintf("unknown-0x%08x", int(x))
	}
}

func (x HeaderExtensionType) MarshalText() ([]byte, error) {
	return []byte(x.String()), nil
}

type HeaderExtension struct {
	Type   HeaderExtensionType `json:"type"`
	Length uint32              `json:"length"`
	Data   interface{}         `json:"data,omitempty"`
}

type FeatureNameTableEntryType uint8

const (
	FeatureNameTableEntryTypeIncompatible = FeatureNameTableEntryType(0)
	FeatureNameTableEntryTypeCompatible   = FeatureNameTableEntryType(1)
	FeatureNameTableEntryTypeAutoclear    = FeatureNameTableEntryType(2)
)

func (x FeatureNameTableEntryType) String() string {
	switch x {
	case FeatureNameTableEntryTypeIncompatible:
		return "incompatible"
	case FeatureNameTableEntryTypeCompatible:
		return "compatible"
	case FeatureNameTableEntryTypeAutoclear:
		return "autoclear"
	default:
		return fmt.Sprintf("unknown-%d", int(x))
	}
}

func (x FeatureNameTableEntryType) MarshalText() ([]byte, error) {
	return []byte(x.String()), nil
}

type FeatureName [46]byte

type FeatureNameTableEntry struct {
	Type FeatureNameTableEntryType `json:"type"`
	Bit  uint8                     `json:"bit"`
	Name string                    `json:"name"`
}

type OffsetLengthPair64 struct {
	Offset uint64 `json:"offset"`
	Length uint64 `json:"length"`
}

var (
	ErrNotQcow2               = fmt.Errorf("%w: image is not qcow2", image.ErrWrongType)
	ErrUnsupportedBackingFile = errors.New("unsupported backing file")
	ErrUnsupportedEncryption  = errors.New("unsupported encryption method")
	ErrUnsupportedCompression = errors.New("unsupported compression type")
	ErrUnsupportedFeature     = errors.New("unsupported feature")
)

// Readable returns nil if the image is readable, otherwise returns an error.
func (header *Header) Readable() error {
	if string(header.Magic[:]) != Magic {
		return ErrNotQcow2
	}
	if header.Version < 2 {
		return ErrNotQcow2
	}
	if header.ClusterBits < 9 {
		return fmt.Errorf("expected cluster bits >= 9, got %d", header.ClusterBits)
	}
	if header.CryptMethod != CryptMethodNone {
		return fmt.Errorf("%w: %q", ErrUnsupportedEncryption, header.CryptMethod)
	}
	if v3 := header.HeaderFieldsV3; v3 != nil {
		for i := 0; i < 64; i++ {
			if (v3.IncompatibleFeatures>>i)&0b1 == 0b1 {
				switch i {
				case IncompatibleFeaturesDirtyBit, IncompatibleFeaturesCorruptBit:
					log.Warnf("unexpected incompatible feature bit: %q", IncompatibleFeaturesNames[i])
				case IncompatibleFeaturesExtendedL2EntriesBit:
					log.Warnf("Support for %q is experimental", IncompatibleFeaturesNames[i])
				case IncompatibleFeaturesCompressionTypeBit:
					// NOP
				case IncompatibleFeaturesExternalDataFileBit:
					return fmt.Errorf("%w: incompatible feature: %q", ErrUnsupportedFeature, IncompatibleFeaturesNames[i])
				default:
					return fmt.Errorf("%w: incompatible feature bit %d", ErrUnsupportedFeature, i)
				}
			}
		}
	}
	if additional := header.HeaderFieldsAdditional; additional != nil {
		if decompressors[additional.CompressionType] == nil {
			return fmt.Errorf("%w (%q)", ErrUnsupportedCompression, additional.CompressionType)
		}
	}
	return nil
}

func readHeader(r io.Reader) (*Header, error) {
	var header Header
	if err := binary.Read(r, binary.BigEndian, &header.HeaderFieldsV2); err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrNotQcow2, err)
	}
	if string(header.Magic[:]) != Magic {
		return nil, fmt.Errorf("%w (the image lacks magic %q)", ErrNotQcow2, Magic)
	}
	switch header.Version {
	case 0, 1:
		return nil, fmt.Errorf("%w (expected version >= 2, got %d)", ErrNotQcow2, header.HeaderFieldsV2)
	case 2:
		return &header, nil
	}

	var v3 HeaderFieldsV3
	if err := binary.Read(r, binary.BigEndian, &v3); err != nil {
		return nil, err
	}
	header.HeaderFieldsV3 = &v3

	var additional HeaderFieldsAdditional
	if header.HeaderLength > 104 {
		if err := binary.Read(r, binary.BigEndian, &additional); err != nil {
			return nil, err
		}
	}
	header.HeaderFieldsAdditional = &additional
	return &header, nil
}

func readHeaderExtensions(ra io.ReaderAt, header *Header) ([]HeaderExtension, error) {
	var res []HeaderExtension
	r := io.NewSectionReader(ra, int64(header.Length()), -1)
loop:
	for {
		var ext HeaderExtension
		if err := binary.Read(r, binary.BigEndian, &ext.Type); err != nil {
			return res, err
		}
		if err := binary.Read(r, binary.BigEndian, &ext.Length); err != nil {
			return res, err
		}
		if ext.Length > 4096 {
			log.Warnf("Ignoring header extension %q: too long (%d bytes > 4096 bytes)", ext.Type, ext.Length)
		} else {
			bufLen := align.Up(int(ext.Length), 8)
			buf := make([]byte, bufLen)
			if _, err := r.Read(buf); err != nil {
				return res, err
			}
			data := buf[:ext.Length]
			switch ext.Type {
			case HeaderExtensionTypeEnd:
				break loop
			case HeaderExtensionTypeBackingFileFormatNameString,
				HeaderExtensionTypeExternalDataFileNameString:
				ext.Data = string(data)
			case HeaderExtensionTypeFeatureNameTable:
				names := int(ext.Length) / 48
				var nameTable []FeatureNameTableEntry
				for i := 0; i < names; i++ {
					ent := FeatureNameTableEntry{
						Type: FeatureNameTableEntryType(data[(i * 48)]),
						Bit:  uint8(data[(i*48)+1]),
						Name: string(bytes.Trim(data[(i*48)+2:(i*48)+48], "\x00")),
					}
					nameTable = append(nameTable, ent)
				}
				ext.Data = nameTable
			case HeaderExtensionTypeFullDiskEncryptionHeaderPointer:
				var ptr OffsetLengthPair64
				if err := binary.Read(bytes.NewReader(data), binary.BigEndian, &ptr); err != nil {
					return res, err
				}
				ext.Data = &ptr
			case HeaderExtensionTypeBitmapsExtension:
				// NOP
			default:
				ext.Data = data
			}
		}
		res = append(res, ext)
		if len(res) > 256 {
			return res, fmt.Errorf("too many header extensions (%d entries)", len(res))
		}
	}
	return res, nil
}

type l1TableEntry uint64

// l2Offset returns the offset into the image file at which the L2 table starts.
func (x l1TableEntry) l2Offset() uint64 {
	return uint64(x) & 0x00fffffffffffe00
}

func readL1Table(ra io.ReaderAt, offset uint64, entries uint32) ([]l1TableEntry, error) {
	if offset == 0 {
		return nil, errors.New("invalid L1 table offset: 0")
	}
	if entries == 0 {
		return nil, errors.New("invalid L1 table size: 0")
	}
	r := io.NewSectionReader(ra, int64(offset), int64(entries*8))
	l1Table := make([]l1TableEntry, entries)
	if err := binary.Read(r, binary.BigEndian, &l1Table); err != nil {
		return nil, err
	}
	return l1Table, nil
}

type l2TableEntry uint64

func (x l2TableEntry) clusterDescriptor() uint64 {
	return uint64(x) & 0x3fffffffffffffff
}

func (x l2TableEntry) compressed() bool {
	return (x>>62)&0b1 == 0b1
}

// extendedL2TableEntry is not supported yet
type extendedL2TableEntry struct {
	L2TableEntry l2TableEntry
	// the following bitmaps are meaningless for compressed clusters
	ZeroStatusBitmap  uint32 // 1: reads as zeros, 0: no effect
	AllocStatusBitmap uint32 // 1: allocated, 0: not allocated
}

func readL2Table(ra io.ReaderAt, offset uint64, clusterSize int) ([]l2TableEntry, error) {
	if offset == 0 {
		return nil, errors.New("invalid L2 table offset: 0")
	}
	r := io.NewSectionReader(ra, int64(offset), int64(clusterSize))
	entries := clusterSize / 8
	l2Table := make([]l2TableEntry, entries)
	if err := binary.Read(r, binary.BigEndian, &l2Table); err != nil {
		return nil, err
	}
	return l2Table, nil
}

func readExtendedL2Table(ra io.ReaderAt, offset uint64, clusterSize int) ([]extendedL2TableEntry, error) {
	if offset == 0 {
		return nil, errors.New("invalid extended L2 table offset: 0")
	}
	r := io.NewSectionReader(ra, int64(offset), int64(clusterSize))
	entries := clusterSize / 16
	extL2Table := make([]extendedL2TableEntry, entries)
	if err := binary.Read(r, binary.BigEndian, &extL2Table); err != nil {
		return nil, err
	}
	return extL2Table, nil
}

type standardClusterDescriptor uint64

func (desc standardClusterDescriptor) allZero() bool {
	return desc&0b1 == 0b1
}

func (desc standardClusterDescriptor) hostClusterOffset() uint64 {
	return uint64(desc) & 0x00fffffffffffe00
}

type compressedClusterDescriptor uint64

func (desc compressedClusterDescriptor) x(clusterBits int) int {
	return 62 - (clusterBits - 8)
}

func (desc compressedClusterDescriptor) hostClusterOffset(clusterBits int) uint64 {
	x := desc.x(clusterBits)
	mask := uint64((1 << x) - 1)
	return uint64(desc) & mask
}

func (desc compressedClusterDescriptor) additionalSectors(clusterBits int) int {
	x := desc.x(clusterBits)
	return int(uint64(desc) >> x)
}

// Qcow2 implements [image.Image].
type Qcow2 struct {
	ra                  io.ReaderAt
	*Header             `json:"header"`
	HeaderExtensions    []HeaderExtension `json:"header_extensions"`
	errUnreadable       error
	clusterSize         int
	l2Entries           int
	l1Table             []l1TableEntry
	l2TableCache        *lru.Cache[l1TableEntry, []l2TableEntry]
	decompressor        Decompressor
	BackingFile         string     `json:"backing_file"`
	BackingFileFullPath string     `json:"backing_file_full_path"`
	BackingFileFormat   image.Type `json:"backing_file_format"`
	backingImage        image.Image
}

// With the default cluster size (64 Kib) this uses 1 MiB and cover 8 GiB image.
const maxL2Tables = 16

// Open opens an qcow2 image.
//
// To open an image with backing files, ra must implement [Namer],
// and openWithType must be non-nil.
func Open(ra io.ReaderAt, openWithType image.OpenWithType) (*Qcow2, error) {
	img := &Qcow2{
		ra:           ra,
		l2TableCache: lru.New[l1TableEntry, []l2TableEntry](maxL2Tables),
	}
	r := io.NewSectionReader(ra, 0, -1)
	var err error
	img.Header, err = readHeader(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}
	img.errUnreadable = img.Header.Readable() // cache
	if img.errUnreadable == nil {
		// Load cluster size
		img.clusterSize = 1 << img.ClusterBits

		// Load header extensions
		img.HeaderExtensions, err = readHeaderExtensions(ra, img.Header)
		if err != nil {
			log.Warnf("Failed to read header extensions: %v", err)
		}
		for _, ext := range img.HeaderExtensions {
			switch ext.Type {
			case HeaderExtensionTypeBackingFileFormatNameString:
				backingFileFormat, ok := ext.Data.(string)
				if !ok {
					log.Warnf("Unexpected header extension %v", ext)
					break
				}
				img.BackingFileFormat = image.Type(backingFileFormat)
			}
		}

		// Used to get cluster metadata.
		if img.extendedL2() {
			img.l2Entries = img.clusterSize / 16
		} else {
			img.l2Entries = img.clusterSize / 8
		}

		// Load L1 table
		img.l1Table, err = readL1Table(ra, img.L1TableOffset, img.L1Size)
		if err != nil {
			return img, fmt.Errorf("failed to read L1 table: %w", err)
		}

		// Load decompressor
		var compressionType CompressionType
		if img.HeaderFieldsAdditional != nil {
			compressionType = img.CompressionType
		}
		img.decompressor = decompressors[compressionType]
		if img.decompressor == nil {
			img.errUnreadable = fmt.Errorf("%w (no decompressor is registered for compression type %v)", ErrUnsupportedCompression, compressionType)
			return img, nil
		}

		// Load backing file
		if img.BackingFileOffset != 0 {
			if img.BackingFileSize > 1023 {
				img.errUnreadable = fmt.Errorf("expected backing file offset <= 1023, got %d", img.BackingFileSize)
				return img, nil
			}
			backingFileNameB := make([]byte, img.BackingFileSize)
			if _, err = ra.ReadAt(backingFileNameB, int64(img.BackingFileOffset)); err != nil {
				img.errUnreadable = fmt.Errorf("failed to read backing file name: %w", err)
				return img, nil
			}
			img.BackingFile = string(backingFileNameB)
			img.BackingFileFullPath, err = resolveBackingFilePath(ra, img.BackingFile)
			if err != nil {
				img.errUnreadable = fmt.Errorf("%w: failed to resolve the path of %q: %v", ErrUnsupportedBackingFile, img.BackingFile, err)
				return img, nil
			}
			backingFile, err := os.Open(img.BackingFileFullPath)
			if err != nil {
				img.errUnreadable = fmt.Errorf("%w (file %q): %v", ErrUnsupportedBackingFile, img.BackingFileFullPath, err)
				return img, nil
			}
			img.backingImage, err = openWithType(backingFile, img.BackingFileFormat)
			if err != nil {
				img.errUnreadable = fmt.Errorf("%w (file %q, format %q): %v", ErrUnsupportedBackingFile, img.BackingFileFullPath, img.BackingFileFormat, err)
				_ = img.backingImage.Close()
				return img, nil
			}
		}
	}
	return img, nil
}

// Namer is implemented by [os.File].
type Namer interface {
	Name() string
}

func resolveBackingFilePath(ra io.ReaderAt, s string) (string, error) {
	if filepath.IsAbs(s) {
		return s, nil
	}
	raAsNamer, ok := ra.(Namer)
	if !ok {
		return "", fmt.Errorf("file %T does not have Name() string", ra)
	}
	dir := filepath.Dir(raAsNamer.Name())
	// s can be "../../..." (allowed by qemu)
	joined := filepath.Join(dir, s)
	return filepath.Abs(joined)
}

func (img *Qcow2) Close() error {
	var err error
	if img.backingImage != nil {
		err = img.backingImage.Close()
	}
	if closer, ok := img.ra.(io.Closer); ok {
		if err2 := closer.Close(); err2 != nil {
			if err != nil {
				log.Warn(err)
			}
			err = err2
		}
	}
	return err
}

func (img *Qcow2) Type() image.Type {
	return Type
}

func (img *Qcow2) Size() int64 {
	return int64(img.Header.Size)
}

func (img *Qcow2) Readable() error {
	return img.errUnreadable
}

func (img *Qcow2) extendedL2() bool {
	return img.HeaderFieldsV3 != nil && img.IncompatibleFeatures&(1<<IncompatibleFeaturesExtendedL2EntriesBit) != 0
}

func (img *Qcow2) getL2Table(l1Entry l1TableEntry) ([]l2TableEntry, error) {
	l2Table, ok := img.l2TableCache.Get(l1Entry)
	if !ok {
		var err error
		l2Table, err = readL2Table(img.ra, l1Entry.l2Offset(), img.clusterSize)
		if err != nil {
			return nil, err
		}
		img.l2TableCache.Add(l1Entry, l2Table)
	}
	return l2Table, nil
}

type clusterMeta struct {
	// L1 info.
	L1Index int
	L1Entry l1TableEntry

	// L2 info.
	L2Index int
	L2Entry l2TableEntry

	// Extended L2 info.
	ExtL2Entry extendedL2TableEntry

	// Cluster is not allocated in this image, but it may be allocated in the
	// backing file.
	Allocated bool

	// Cluster is present in this image and is compressed.
	Compressed bool

	// Cluster is present in this image and is all zeros.
	Zero bool
}

func (img *Qcow2) getClusterMeta(off int64, cm *clusterMeta) error {
	clusterNo := off / int64(img.clusterSize)
	cm.L1Index = int(clusterNo / int64(img.l2Entries))
	if cm.L1Index >= len(img.l1Table) {
		return fmt.Errorf("index %d exceeds the L1 table length %d", cm.L1Index, len(img.l1Table))
	}

	cm.L1Entry = img.l1Table[cm.L1Index]
	l2TableOffset := cm.L1Entry.l2Offset()
	if l2TableOffset == 0 {
		return nil
	}

	cm.L2Index = int(clusterNo % int64(img.l2Entries))

	if img.extendedL2() {
		// TODO
		extL2Table, err := readExtendedL2Table(img.ra, l2TableOffset, img.clusterSize)
		if err != nil {
			return fmt.Errorf("failed to read extended L2 table for L1 entry %v (index %d): %w", cm.L1Entry, cm.L1Index, err)
		}
		if cm.L2Index >= len(extL2Table) {
			return fmt.Errorf("index %d exceeds the extended L2 table length %d", cm.L2Index, len(extL2Table))
		}
		cm.ExtL2Entry = extL2Table[cm.L2Index]
		cm.L2Entry = cm.ExtL2Entry.L2TableEntry
	} else {
		l2Table, err := img.getL2Table(cm.L1Entry)
		if err != nil {
			return fmt.Errorf("failed to read L2 table for L1 entry %v (index %d): %w", cm.L1Entry, cm.L1Index, err)
		}
		if cm.L2Index >= len(l2Table) {
			return fmt.Errorf("index %d exceeds the L2 table length %d", cm.L2Index, len(l2Table))
		}
		cm.L2Entry = l2Table[cm.L2Index]
	}

	desc := cm.L2Entry.clusterDescriptor()
	if desc == 0 && !img.extendedL2() {
		return nil
	}

	cm.Allocated = true
	if cm.L2Entry.compressed() {
		cm.Compressed = true
	} else {
		// When using extended L2 clusters this is always false. To find which sub
		// cluster is allocated/zero we need to iterate over the allocation bitmap in
		// the extended l2 cluster entry.
		cm.Zero = standardClusterDescriptor(desc).allZero()
	}

	return nil
}

// readAtAligned requires that off and off+len(p)-1 belong to the same cluster.
func (img *Qcow2) readAtAligned(p []byte, off int64) (int, error) {
	var cm clusterMeta
	if err := img.getClusterMeta(off, &cm); err != nil {
		return 0, err
	}
	if !cm.Allocated {
		return img.readAtAlignedUnallocated(p, off)
	}
	var (
		n   int
		err error
	)
	desc := cm.L2Entry.clusterDescriptor()
	if cm.Compressed {
		compressedDesc := compressedClusterDescriptor(desc)
		n, err = img.readAtAlignedCompressed(p, off, compressedDesc)
		if err != nil {
			err = fmt.Errorf("failed to read compressed cluster (len=%d, off=%d, desc=0x%X): %w", len(p), off, desc, err)
		}
	} else {
		standardDesc := standardClusterDescriptor(desc)
		if img.extendedL2() {
			n, err = img.readAtAlignedStandardExtendedL2(p, off, standardDesc, cm.ExtL2Entry)
			if err != nil {
				err = fmt.Errorf("failed to read standard cluster with Extended L2 (len=%d, off=%d, desc=0x%X): %w", len(p), off, desc, err)
			}
		} else {
			n, err = img.readAtAlignedStandard(p, off, standardDesc)
			if err != nil {
				err = fmt.Errorf("failed to read standard cluster (len=%d, off=%d, desc=0x%X): %w", len(p), off, desc, err)
			}
		}
	}
	return n, err
}

func (img *Qcow2) readAtAlignedUnallocated(p []byte, off int64) (int, error) {
	if img.backingImage == nil {
		return img.readZero(p, off)
	}
	n, err := img.backingImage.ReadAt(p, off)
	var consumed int
	if n > 0 {
		consumed += n
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if remaining := len(p) - n; remaining > 0 {
		readZeroN, readZeroErr := img.readZero(p[consumed:consumed+remaining], off+int64(consumed))
		if readZeroN > 0 {
			consumed += readZeroN
		}
		if err == nil && readZeroErr != nil {
			err = readZeroErr
		}
	}
	return consumed, err
}

func (img *Qcow2) readAtAlignedStandard(p []byte, off int64, desc standardClusterDescriptor) (int, error) {
	if desc.allZero() {
		return img.readZero(p, off)
	}
	hostClusterOffset := desc.hostClusterOffset()
	rawOffset := int64(desc.hostClusterOffset()) + (off % int64(img.clusterSize))
	if rawOffset == 0 {
		return 0, fmt.Errorf("invalid raw offset 0 for virtual offset %d (host cluster offset=%d)", off, hostClusterOffset)
	}
	n, err := img.ra.ReadAt(p, rawOffset)
	if err != nil {
		err = fmt.Errorf("failed to read %d bytes from the raw offset %d: %w", len(p), rawOffset, err)
	}
	return n, err
}

// readAtAlignedStandardExtendedL2 is experimental
//
// TODO: read multiple subclusters at once
//
// clusterNo = offset / clusterSize
// subclusterNo = (offset % clusterSize) / subclusterSize
func (img *Qcow2) readAtAlignedStandardExtendedL2(p []byte, off int64, desc standardClusterDescriptor, extL2Entry extendedL2TableEntry) (int, error) {
	var n int
	subclusterSize := img.clusterSize / 32
	hostClusterOffset := desc.hostClusterOffset()
	subclusterNoBegin := (int(off) % img.clusterSize) / subclusterSize
	for i := subclusterNoBegin; i < 32; i++ { // i is the subcluster number
		currentOff := off + int64(n)
		clusterNo := currentOff / int64(img.clusterSize)
		clusterBegin := clusterNo * int64(img.clusterSize)
		subclusterBegin := clusterBegin + int64(i)*int64(subclusterSize)
		subclusterEnd := subclusterBegin + int64(subclusterSize)
		readSize := subclusterEnd - currentOff

		pIdxBegin := n
		pIdxEnd := n + int(readSize)
		if pIdxEnd > len(p) {
			pIdxEnd = len(p)
		}
		if pIdxEnd <= pIdxBegin {
			break
		}
		var (
			currentN int
			err      error
		)
		if ((extL2Entry.AllocStatusBitmap >> i) & 0b1) == 0b1 {
			currentRawOff := int64(hostClusterOffset) + (off % int64(img.clusterSize)) + int64(n)
			currentN, err = img.ra.ReadAt(p[pIdxBegin:pIdxEnd], currentRawOff)
			if err != nil {
				return n, fmt.Errorf("failed to read from the raw offset %d: %w", currentRawOff, err)
			}
		} else {
			if ((extL2Entry.ZeroStatusBitmap >> i) & 0b1) == 0b1 {
				currentN, err = img.readZero(p[pIdxBegin:pIdxEnd], currentOff)
				if err != nil {
					return n, fmt.Errorf("failed to read zero: %w", err)
				}
			} else {
				currentN, err = img.readAtAlignedUnallocated(p[pIdxBegin:pIdxEnd], currentOff)
				if err != nil && !errors.Is(err, io.EOF) {
					return n, fmt.Errorf("failed to read unallocated: %w", err)
				}
			}
		}
		if currentN > 0 {
			n += currentN
		}
	}
	return n, nil
}

func (img *Qcow2) readAtAlignedCompressed(p []byte, off int64, desc compressedClusterDescriptor) (int, error) {
	hostClusterOffset := desc.hostClusterOffset(int(img.ClusterBits))
	if hostClusterOffset == 0 {
		return 0, fmt.Errorf("invalid host cluster offset 0 for virtual offset %d", off)
	}
	additionalSectors := desc.additionalSectors(int(img.ClusterBits))
	compressedSize := img.clusterSize + 512*additionalSectors
	compressedSR := io.NewSectionReader(img.ra, int64(hostClusterOffset), int64(compressedSize))
	zr, err := img.decompressor(compressedSR)
	if err != nil {
		return 0, fmt.Errorf("could not open the decompressor: %w", err)
	}
	defer zr.Close() //nolint:errcheck
	if discard := off % int64(img.clusterSize); discard != 0 {
		if _, err := io.CopyN(io.Discard, zr, discard); err != nil {
			return 0, err
		}
	}
	return zr.Read(p)
}

func (img *Qcow2) readZero(p []byte, off int64) (int, error) {
	return readZero(p, off, img.Header.Size)
}

func readZero(p []byte, off int64, sz uint64) (int, error) {
	var err error
	l := len(p)
	// If the n = len(p) bytes returned by ReadAt are at the end of the input
	// source, ReadAt may return either err == EOF or err == nil. Returning io.EOF
	// seems to confuse io.SectionReader so we return EOF only for out of bound
	// request.
	if uint64(off+int64(l)) > sz {
		l = int(sz - uint64(off))
		if l < 0 {
			l = 0
		}
		err = io.EOF
		p = p[:l]
	}

	// Optimized by the compiler to memclr call.
	// https://go-review.googlesource.com/c/go/+/2520
	for i := range p {
		p[i] = 0
	}

	return l, err
}

// clusterStatus returns an extent describing a single cluster. off must be aligned to
// cluster size.
func (img *Qcow2) clusterStatus(off int64) (image.Extent, error) {
	var cm clusterMeta
	if err := img.getClusterMeta(off, &cm); err != nil {
		return image.Extent{}, err
	}

	if !cm.Allocated {
		// If there is no backing file, or the cluster cannot be in the backing file,
		// return an unallocated cluster.
		if img.backingImage == nil || off >= img.backingImage.Size() {
			// Unallocated cluster reads as zeros.
			unallocated := image.Extent{Start: off, Length: int64(img.clusterSize), Zero: true}
			return unallocated, nil
		}

		// Get the cluster from the backing file.
		length := int64(img.clusterSize)
		if off+length > img.backingImage.Size() {
			length = img.backingImage.Size() - off
		}
		parent, err := img.backingImage.Extent(off, length)
		if err != nil {
			return parent, err
		}
		// The backing image may be a raw image not aligned to cluster size.
		parent.Length = int64(img.clusterSize)
		return parent, nil
	}

	// Cluster present in this image.
	allocated := image.Extent{
		Start:      off,
		Length:     int64(img.clusterSize),
		Allocated:  true,
		Compressed: cm.Compressed,
		Zero:       cm.Zero,
	}
	return allocated, nil
}

// Return true if extents have the same status.
func sameStatus(a, b image.Extent) bool {
	return a.Allocated == b.Allocated && a.Zero == b.Zero && a.Compressed == b.Compressed
}

// Extent returns the next extent starting at the specified offset. An extent
// describes one or more clusters having the same status. The maximum length of
// the returned extent is limited by the specified length. The minimum length of
// the returned extent is length of one cluster.
func (img *Qcow2) Extent(start, length int64) (image.Extent, error) {
	// Default to zero length non-existent cluster.
	var current image.Extent

	if img.errUnreadable != nil {
		return current, img.errUnreadable
	}
	if img.clusterSize == 0 {
		return current, errors.New("cluster size cannot be 0")
	}
	if start+length > int64(img.Header.Size) {
		return current, errors.New("length out of bounds")
	}

	// Compute the clusterStart of the first cluster to query. This may be behind start.
	clusterStart := start / int64(img.clusterSize) * int64(img.clusterSize)

	remaining := length
	for remaining > 0 {
		clusterStatus, err := img.clusterStatus(clusterStart)
		if err != nil {
			return current, err
		}

		// First cluster: if start is not aligned to cluster size, clip the start.
		if clusterStatus.Start < start {
			clusterStatus.Start = start
			clusterStatus.Length -= start - clusterStatus.Start
		}

		// Last cluster: if start+length is not aligned to cluster size, clip the end.
		if remaining < int64(img.clusterSize) {
			clusterStatus.Length -= int64(img.clusterSize) - remaining
		}

		if current.Length == 0 {
			// First cluster: copy status to current.
			current = clusterStatus
		} else if sameStatus(current, clusterStatus) {
			// Cluster with same status: extend current.
			current.Length += clusterStatus.Length
		} else {
			// Start of next extent
			break
		}

		clusterStart += int64(img.clusterSize)
		remaining -= clusterStatus.Length
	}

	return current, nil
}

// ReadAt implements [io.ReaderAt].
func (img *Qcow2) ReadAt(p []byte, off int64) (n int, err error) {
	if img.errUnreadable != nil {
		err = img.errUnreadable
		return
	}
	if img.clusterSize == 0 {
		err = errors.New("cluster size cannot be 0")
		return
	}
	if len(p) == 0 {
		return
	}
	remaining := len(p)
	var eof bool
	if uint64(off+int64(remaining)) >= img.Header.Size {
		remaining = int(img.Header.Size - uint64(off))
		eof = true
	}

	for remaining > 0 {
		currentOff := off + int64(n)
		clusterNo := currentOff / int64(img.clusterSize)
		clusterBegin := clusterNo * int64(img.clusterSize)
		clusterEnd := clusterBegin + int64(img.clusterSize)
		readSize := clusterEnd - currentOff

		pIndexBegin := n
		pIndexEnd := n + int(readSize)
		if pIndexEnd > len(p) {
			pIndexEnd = len(p)
		}
		var currentN int
		currentN, err = img.readAtAligned(p[pIndexBegin:pIndexEnd], currentOff)
		if currentN == 0 && err == nil {
			err = io.EOF
		}
		if currentN > 0 {
			n += currentN
			remaining -= currentN
		}
		if err != nil {
			break
		}
	}

	if err == nil && eof {
		err = io.EOF
	}
	return
}
