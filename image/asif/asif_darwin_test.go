package asif

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestOpenASIF(t *testing.T) {
	// Check macOS version
	if productVersion, err := exec.CommandContext(t.Context(), "sw_vers", "--productVersion").Output(); err != nil {
		t.Fatalf("failed to get product version: %v", err)
	} else if majorVersion, err := strconv.ParseInt(strings.Split(string(productVersion), ".")[0], 10, 64); err != nil {
		t.Fatalf("failed to parse product version: %v", err)
	} else if majorVersion < 26 {
		t.Skipf("skipping test on macOS version < 26: %s", productVersion)
	}

	tempDir := t.TempDir()
	asifFilePath := filepath.Join(tempDir, "diffdisk.asif")

	// Create a blank ASIF disk image using diskutil
	if err := exec.CommandContext(t.Context(), "diskutil", "image", "create", "blank", "--fs", "none", "--format", "ASIF", "--size", "100GiB", asifFilePath).Run(); err != nil {
		t.Fatalf("failed to create disk image: %v", err)
	}

	// Get disk image info using diskutil
	var sectorCount uint64
	var totalBytes int64
	out, err := exec.CommandContext(t.Context(), "diskutil", "image", "info", asifFilePath).Output()
	if err != nil {
		t.Fatalf("failed to get disk image info: %v", err)
	}

	// Parse sector count from the output
	reSectorCount := regexp.MustCompile(`Sector Count: (\d+)`)
	if sectorCountMatch := reSectorCount.FindStringSubmatch(string(out)); len(sectorCountMatch) != 2 {
		t.Fatalf("failed to parse sector count from disk image info")
	} else if parsedSectorCount, err := strconv.ParseUint(sectorCountMatch[1], 10, 64); err != nil {
		t.Fatalf("failed to parse sector count: %v", err)
	} else {
		sectorCount = parsedSectorCount
	}

	// Block size is not included in the output of `diskutil image info`

	// Parse total bytes from the output
	reTotalBytes := regexp.MustCompile(`Total Bytes: (\d+)`)
	if totalBytesMatch := reTotalBytes.FindStringSubmatch(string(out)); len(totalBytesMatch) != 2 {
		t.Fatalf("failed to parse block size from disk image info")
	} else if parsedTotalBytes, err := strconv.ParseInt(totalBytesMatch[1], 10, 64); err != nil {
		t.Fatalf("failed to parse block size: %v", err)
	} else {
		totalBytes = parsedTotalBytes
	}

	// Open the ASIF image
	f, err := os.Open(asifFilePath)
	if err != nil {
		t.Fatalf("failed to open ASIF file: %v", err)
	}
	defer f.Close() //nolint:errcheck

	// Open ASIF image and verify properties
	if img, err := Open(f); err != nil {
		t.Fatalf("failed to open ASIF image: %v", err)
	} else if img.sectorCount != sectorCount {
		t.Fatalf("unexpected sector count: got %d, want %d", img.sectorCount, sectorCount)
	} else if img.Size() != totalBytes {
		t.Fatalf("unexpected size: got %d, want %d", img.Size(), totalBytes)
	}
}
