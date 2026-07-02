// Package image fingerprints and classifies IOL image files.
//
// An IOL image is a Linux ELF binary. This package derives:
//   - id: first 16 hex chars of the file's sha256
//   - arch: i386 (32-bit) vs x86_64 (64-bit), parsed from the ELF header
//   - class: l2 (switching) vs l3 (routing), sniffed heuristically
//
// The class heuristic (verified against real 17.18.02 binaries): L2 images
// embed their build platform string containing "linux_l2"
// ("x86_64_crb_linux_l2-adventerprisek9-ms"; legacy 32-bit "i86bi_linux_l2-");
// L3 images contain no "linux_l2" at all. Generic switching strings
// ("spanning-tree", "switchport", ...) appear in BOTH classes, so they can't
// discriminate. The marker sits >100 MB into real images, so the whole file is
// scanned — piggybacked on the sha256 read. This is a heuristic hint only —
// the lab schema treats class as advisory and the GUI lets the user override.
package image

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
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

	// sha256 over the whole file, scanning for the L2 build-string marker in
	// the same pass (see SniffClass — the marker sits >100 MB into real L2
	// images, so no bounded prefix can catch it).
	h := sha256.New()
	scan := newL2Scanner()
	if _, err := io.Copy(io.MultiWriter(h, scan), f); err != nil {
		return nil, err
	}
	sum := hex.EncodeToString(h.Sum(nil))

	// Rewind and read a small prefix for ELF header parsing only.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	prefix := make([]byte, min64(fi.Size(), 4096))
	if _, err := io.ReadFull(f, prefix); err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	arch := ParseArch(prefix)
	class := classify(scan.found, baseName(path), fi.Size())

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

// l2Needle is the discriminating substring of the L2 image's embedded build
// platform string: 64-bit L2 IOL embeds "x86_64_crb_linux_l2-adventerprisek9-ms"
// and the legacy 32-bit generation is named/embeds "i86bi_linux_l2-...". L3
// images contain NO "linux_l2" occurrence at all (verified against real
// 17.18.02 L3+L2 binaries) — whereas generic switching strings
// ("spanning-tree", "switchport", ...) appear in BOTH, so marker-presence
// heuristics cannot tell the classes apart.
var l2Needle = []byte("linux_l2")

// l2Scanner is an io.Writer that watches a byte stream for l2Needle, keeping
// a needle-sized tail between writes so matches spanning chunk boundaries are
// still found. Feed it via io.MultiWriter alongside the sha256 hasher, so
// classification costs no extra file pass.
type l2Scanner struct {
	tail  []byte
	found bool
}

func newL2Scanner() *l2Scanner {
	return &l2Scanner{tail: make([]byte, 0, len(l2Needle)-1)}
}

func (s *l2Scanner) Write(p []byte) (int, error) {
	if !s.found {
		if bytes.Contains(p, l2Needle) || bytes.Contains(append(s.tail, p[:min(len(p), len(l2Needle)-1)]...), l2Needle) {
			s.found = true
		} else {
			keep := len(l2Needle) - 1
			if len(p) >= keep {
				s.tail = append(s.tail[:0], p[len(p)-keep:]...)
			} else {
				s.tail = append(s.tail, p...)
				if len(s.tail) > keep {
					s.tail = append(s.tail[:0], s.tail[len(s.tail)-keep:]...)
				}
			}
		}
	}
	return len(p), nil
}

// classify decides the image class from the full-file scan plus a filename
// hint fallback ("l2" in the basename, for stripped/repacked images that lost
// the build string). Heuristic; advisory only.
func classify(foundL2Marker bool, filename string, size int64) Class {
	if size < 1024 {
		return ClassUnknown
	}
	if foundL2Marker {
		return ClassL2
	}
	if strings.Contains(strings.ToLower(filename), "l2") {
		return ClassL2
	}
	return ClassL3
}

// SniffClass classifies an in-memory image buffer; kept for tests and small
// callers. Same rules as the streaming path, minus the filename hint.
func SniffClass(b []byte) Class {
	if len(b) < 1024 {
		return ClassUnknown
	}
	if bytes.Contains(b, l2Needle) {
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
