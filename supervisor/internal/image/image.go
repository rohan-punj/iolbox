// Package image fingerprints and classifies IOL image files.
//
// An IOL image is a Linux ELF binary. This package derives:
//   - id: first 16 hex chars of the file's sha256
//   - arch: i386 (32-bit) vs x86_64 (64-bit), parsed from the ELF header
//   - class: l2 (switching) vs l3 (routing), sniffed heuristically
//
// The class heuristic (documented, adjustable): L2 IOL images contain switching
// strings such as "l2" markers ("Switching", "vlan", "spanning-tree",
// "mac-address-table") far more densely than L3-only images. We scan the binary
// for a set of L2-indicative substrings; presence of several implies l2. This is
// a heuristic hint only — the lab schema treats class as advisory and the GUI
// lets the user override.
package image

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Arch is the CPU architecture of an IOL ELF binary.
type Arch string

const (
	// ArchI386 is 32-bit x86.
	ArchI386 Arch = "i386"
	// ArchX8664 is 64-bit x86-64.
	ArchX8664 Arch = "x86_64"
	// ArchUnknown means the ELF machine/class was not recognized.
	ArchUnknown Arch = "unknown"
)

// Class is the IOL image role hint.
type Class string

const (
	// ClassL2 is an L2 (switching) IOL image.
	ClassL2 Class = "l2"
	// ClassL3 is an L3 (routing) IOL image.
	ClassL3 Class = "l3"
	// ClassUnknown means the class could not be determined.
	ClassUnknown Class = "unknown"
)

// Info is the fingerprint + classification of an image file.
type Info struct {
	ID       string
	Filename string
	SHA256   string
	Size     int64
	Arch     Arch
	Class    Class
}

// Inspect fingerprints and classifies the file at path.
func Inspect(path string) (*Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// sha256 over the whole file.
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	sum := hex.EncodeToString(h.Sum(nil))

	// Rewind and read a prefix for ELF + class sniffing. IOL binaries are a few
	// MB; a generous prefix captures the string table cheaply. We cap the read
	// so we never slurp an enormous file into memory.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	const sniffLimit = 8 << 20 // 8 MiB
	prefix := make([]byte, min64(fi.Size(), sniffLimit))
	if _, err := io.ReadFull(f, prefix); err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	arch := ParseArch(prefix)
	class := SniffClass(prefix)

	return &Info{
		ID:       sum[:16],
		Filename: baseName(path),
		SHA256:   sum,
		Size:     fi.Size(),
		Arch:     arch,
		Class:    class,
	}, nil
}

// ParseArch reads the ELF identification bytes to determine 32- vs 64-bit x86.
// It looks at EI_CLASS (byte 4: 1=ELF32, 2=ELF64) and e_machine (offset 18,
// endianness per EI_DATA byte 5): EM_386=3, EM_X86_64=62.
func ParseArch(b []byte) Arch {
	if len(b) < 20 || b[0] != 0x7f || b[1] != 'E' || b[2] != 'L' || b[3] != 'F' {
		return ArchUnknown
	}
	eiClass := b[4] // 1=32-bit, 2=64-bit
	eiData := b[5]  // 1=little-endian, 2=big-endian
	var machine uint16
	switch eiData {
	case 1:
		machine = binary.LittleEndian.Uint16(b[18:20])
	case 2:
		machine = binary.BigEndian.Uint16(b[18:20])
	default:
		return ArchUnknown
	}
	const (
		emX86    = 3  // EM_386
		emX86_64 = 62 // EM_X86_64
	)
	switch {
	case machine == emX86 && eiClass == 1:
		return ArchI386
	case machine == emX86_64 && eiClass == 2:
		return ArchX8664
	default:
		return ArchUnknown
	}
}

// l2Markers are substrings that appear densely in L2/switching IOL images.
var l2Markers = [][]byte{
	[]byte("spanning-tree"),
	[]byte("mac-address-table"),
	[]byte("switchport"),
	[]byte("vlan"),
	[]byte("Switching"),
}

// SniffClass returns l2 when several switching markers are present, otherwise
// l3, or unknown if the buffer is too small to judge. Heuristic; advisory only.
func SniffClass(b []byte) Class {
	if len(b) < 1024 {
		return ClassUnknown
	}
	hits := 0
	for _, m := range l2Markers {
		if bytes.Contains(b, m) {
			hits++
		}
	}
	if hits >= 2 {
		return ClassL2
	}
	return ClassL3
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

// String helpers so Info prints usefully.
func (i *Info) String() string {
	return fmt.Sprintf("image %s (%s, %s, %d bytes)", i.ID, i.Arch, i.Class, i.Size)
}
