package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestTFTPWireFrames(t *testing.T) {
	if got := tftpRequestPacket(tftpRRQ, "config.cfg", "octet"); !bytes.Equal(got, []byte{0, 1, 'c', 'o', 'n', 'f', 'i', 'g', '.', 'c', 'f', 'g', 0, 'o', 'c', 't', 'e', 't', 0}) {
		t.Fatalf("RRQ = %v", got)
	}
	if got := tftpDataPacket(7, []byte("abc")); !bytes.Equal(got, []byte{0, 3, 0, 7, 'a', 'b', 'c'}) {
		t.Fatalf("DATA = %v", got)
	}
	if got := tftpAckPacket(7); !bytes.Equal(got, []byte{0, 4, 0, 7}) {
		t.Fatalf("ACK = %v", got)
	}
	if got := tftpErrorPacket(tftpErrAccess, "denied"); !bytes.Equal(got, []byte{0, 5, 0, 2, 'd', 'e', 'n', 'i', 'e', 'd', 0}) {
		t.Fatalf("ERROR = %v", got)
	}
	if binary.BigEndian.Uint16(tftpDataPacket(65535, nil)[2:4]) != 65535 {
		t.Fatal("block number not big endian")
	}
}

func TestTFTPExactMultipleGetsZeroLengthFinalData(t *testing.T) {
	for size, wantBlocks := range map[int]int{511: 1, 512: 2, 1024: 3} {
		blocks := tftpBlocks(bytes.Repeat([]byte{'x'}, size))
		if len(blocks) != wantBlocks {
			t.Fatalf("size %d blocks=%d want %d", size, len(blocks), wantBlocks)
		}
		if size%512 == 0 && len(blocks[len(blocks)-1]) != 0 {
			t.Fatalf("size %d final DATA was not zero length", size)
		}
	}
}

func TestTFTPSandboxRejectsTraversalSlashNULAndMail(t *testing.T) {
	for _, name := range []string{"../../etc/passwd", "/etc/passwd", "a/b", "a\x00b", "~/.ssh"} {
		if err := validateTFTPName(name); err == nil {
			t.Fatalf("unsafe filename %q accepted", name)
		}
	}
	if _, err := parseTFTPRequest(tftpRequestPacket(tftpRRQ, "file", "mail")); err != nil { // mail is syntactically parsed; the server rejects it with ERROR 4.
		t.Fatalf("mail should parse before policy rejection: %v", err)
	}
}

func TestTFTPSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "link")
	target := filepath.Join(dir, "secret")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	store := NewStore("")
	s := NewTFTPServer(store, filepath.Join(dir, "options.json"))
	s.optionsPath = filepath.Join(dir, "options.json")
	if _, err := s.readFile("link"); err == nil {
		t.Fatal("symlink was served")
	}
}
