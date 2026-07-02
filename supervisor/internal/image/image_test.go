package image

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// elfHeader crafts a minimal 20+ byte ELF identification for arch tests.
func elfHeader(class, data byte, machine uint16) []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x7f, 'E', 'L', 'F'})
	b[4] = class
	b[5] = data
	if data == 2 {
		binary.BigEndian.PutUint16(b[18:20], machine)
	} else {
		binary.LittleEndian.PutUint16(b[18:20], machine)
	}
	return b
}

func TestParseArch(t *testing.T) {
	if got := ParseArch(elfHeader(1, 1, 3)); got != ArchI386 {
		t.Fatalf("i386: %s", got)
	}
	if got := ParseArch(elfHeader(2, 1, 62)); got != ArchX8664 {
		t.Fatalf("x86_64: %s", got)
	}
	// mismatched class/machine -> unknown
	if got := ParseArch(elfHeader(1, 1, 62)); got != ArchUnknown {
		t.Fatalf("mismatch should be unknown, got %s", got)
	}
	// not ELF
	if got := ParseArch([]byte("not an elf file at all")); got != ArchUnknown {
		t.Fatalf("non-elf: %s", got)
	}
	// too short
	if got := ParseArch([]byte{0x7f, 'E'}); got != ArchUnknown {
		t.Fatalf("short: %s", got)
	}
}

func TestSniffClass(t *testing.T) {
	pad := make([]byte, 2048)
	l2 := append(append([]byte{}, pad...), []byte("spanning-tree ... switchport ... vlan")...)
	if got := SniffClass(l2); got != ClassL2 {
		t.Fatalf("l2 expected, got %s", got)
	}
	l3 := append(append([]byte{}, pad...), []byte("ip routing ospf bgp")...)
	if got := SniffClass(l3); got != ClassL3 {
		t.Fatalf("l3 expected, got %s", got)
	}
	if got := SniffClass([]byte("tiny")); got != ClassUnknown {
		t.Fatalf("tiny should be unknown, got %s", got)
	}
}

func TestInspect(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fakeimg.bin")
	content := append(elfHeader(1, 1, 3), make([]byte, 2048)...)
	copy(content[64:], []byte("spanning-tree switchport vlan mac-address-table"))
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Arch != ArchI386 {
		t.Fatalf("arch=%s", info.Arch)
	}
	if info.Class != ClassL2 {
		t.Fatalf("class=%s", info.Class)
	}
	if len(info.ID) != 16 {
		t.Fatalf("id len=%d", len(info.ID))
	}
	if info.Filename != "fakeimg.bin" {
		t.Fatalf("filename=%s", info.Filename)
	}
	if info.SHA256[:16] != info.ID {
		t.Fatalf("id must be sha256 prefix")
	}
}
