package nvram

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	cfgs := []string{
		"",
		"hostname R1\n!\nend\n",
		strings.Repeat("interface Ethernet0/0\n no shutdown\n", 200),
	}
	for _, cfg := range cfgs {
		nv, err := Encode(cfg, Options{})
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if binary.BigEndian.Uint16(nv[0:]) != magicStartup {
			t.Fatalf("bad magic 0x%04X", binary.BigEndian.Uint16(nv[0:]))
		}
		got, err := Decode(nv)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got != cfg {
			t.Fatalf("round-trip mismatch: got %d bytes want %d", len(got), len(cfg))
		}
	}
}

func TestChecksumValid(t *testing.T) {
	nv, _ := Encode("hostname X\n", Options{})
	// Recompute checksum over the section and confirm it lands at 0xFFFF sum.
	end := StartupHeaderLen + int(binary.BigEndian.Uint16(nv[16+2:]))
	_ = end
	length := int(binary.BigEndian.Uint32(nv[16:]))
	section := StartupHeaderLen + length
	var sum uint32
	i := 0
	for i < section-1 {
		sum += uint32(binary.BigEndian.Uint16(nv[i:]))
		i += 2
	}
	if i < section {
		sum += uint32(nv[i]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	if uint16(sum) != 0xFFFF {
		t.Fatalf("checksum invalid: folded sum = 0x%04X (want 0xFFFF)", uint16(sum))
	}
}

func TestEncodeFixedSize(t *testing.T) {
	nv, err := Encode("x", Options{Size: 8192})
	if err != nil {
		t.Fatal(err)
	}
	if len(nv) != 8192 {
		t.Fatalf("size=%d want 8192", len(nv))
	}
	got, _ := Decode(nv)
	if got != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeErrors(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected too-short error")
	}
	bad := make([]byte, 40)
	binary.BigEndian.PutUint16(bad[0:], 0x1234)
	if _, err := Decode(bad); err == nil {
		t.Fatal("expected bad-magic error")
	}
}

func TestPrivateConfig(t *testing.T) {
	nv, err := Encode("startup\n", Options{Private: "private\n"})
	if err != nil {
		t.Fatal(err)
	}
	// Startup still decodes correctly with a private section present.
	got, err := Decode(nv)
	if err != nil || got != "startup\n" {
		t.Fatalf("decode with private: %q %v", got, err)
	}
}
