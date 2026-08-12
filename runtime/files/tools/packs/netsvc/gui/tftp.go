package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	tftpRRQ         uint16 = 1
	tftpWRQ         uint16 = 2
	tftpDATA        uint16 = 3
	tftpACK         uint16 = 4
	tftpERROR       uint16 = 5
	tftpErrNotFound uint16 = 1
	tftpErrAccess   uint16 = 2
	tftpErrIllegal  uint16 = 4
)

type TFTPRequest struct {
	Opcode         uint16
	Filename, Mode string
}
type TFTPLog struct {
	At                                 time.Time
	Filename, Mode, Direction, Outcome string
	Blocks, Bytes                      int
	TID                                int
}

func parseTFTPRequest(b []byte) (TFTPRequest, error) {
	var req TFTPRequest
	if len(b) < 4 {
		return req, errors.New("TFTP request truncated")
	}
	req.Opcode = binary.BigEndian.Uint16(b[:2])
	if req.Opcode != tftpRRQ && req.Opcode != tftpWRQ {
		return req, errors.New("TFTP packet is not RRQ or WRQ")
	}
	parts := strings.Split(string(b[2:]), "\x00")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return req, errors.New("TFTP request needs filename and mode")
	}
	req.Filename, req.Mode = parts[0], strings.ToLower(parts[1])
	if req.Mode != "octet" && req.Mode != "netascii" && req.Mode != "mail" {
		return req, errors.New("unsupported TFTP mode")
	}
	return req, nil
}

func tftpRequestPacket(opcode uint16, filename, mode string) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, opcode)
	b = append(b, filename...)
	b = append(b, 0)
	b = append(b, mode...)
	return append(b, 0)
}
func tftpDataPacket(block uint16, data []byte) []byte {
	b := make([]byte, 4+len(data))
	binary.BigEndian.PutUint16(b, tftpDATA)
	binary.BigEndian.PutUint16(b[2:4], block)
	copy(b[4:], data)
	return b
}
func tftpAckPacket(block uint16) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b, tftpACK)
	binary.BigEndian.PutUint16(b[2:4], block)
	return b
}
func tftpErrorPacket(code uint16, message string) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint16(b, tftpERROR)
	binary.BigEndian.PutUint16(b[2:4], code)
	b = append(b, message...)
	return append(b, 0)
}

func validateTFTPName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, "..") || strings.HasPrefix(name, "~") {
		return errors.New("TFTP access violation: unsafe filename")
	}
	return nil
}

type TFTPServer struct {
	store       *Store
	optionsPath string
	mu          sync.RWMutex
	logs        []TFTPLog
}

func NewTFTPServer(store *Store, optionsPath string) *TFTPServer {
	return &TFTPServer{store: store, optionsPath: optionsPath}
}
func (s *TFTPServer) Logs() []TFTPLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]TFTPLog(nil), s.logs...)
}
func (s *TFTPServer) addLog(row TFTPLog) {
	s.mu.Lock()
	s.logs = append([]TFTPLog{row}, s.logs...)
	if len(s.logs) > 100 {
		s.logs = s.logs[:100]
	}
	s.mu.Unlock()
}

func (s *TFTPServer) handlePacket(control *net.UDPConn, data []byte, addr *net.UDPAddr) {
	req, err := parseTFTPRequest(data)
	if err != nil {
		_, _ = control.WriteToUDP(tftpErrorPacket(tftpErrIllegal, err.Error()), addr)
		return
	}
	if req.Mode == "mail" {
		_, _ = control.WriteToUDP(tftpErrorPacket(tftpErrIllegal, "mail mode is not supported"), addr)
		return
	}
	if err := validateTFTPName(req.Filename); err != nil {
		_, _ = control.WriteToUDP(tftpErrorPacket(tftpErrAccess, err.Error()), addr)
		return
	}
	transfer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return
	}
	go func() {
		defer transfer.Close()
		if req.Opcode == tftpRRQ {
			s.serveRead(transfer, req, addr)
		} else {
			s.serveWrite(transfer, req, addr)
		}
	}()
}

func (s *TFTPServer) serveRead(conn *net.UDPConn, req TFTPRequest, client *net.UDPAddr) {
	data, err := s.readFile(req.Filename)
	if err != nil {
		_, _ = conn.WriteToUDP(tftpErrorPacket(tftpErrNotFound, err.Error()), client)
		s.addLog(TFTPLog{At: time.Now(), Filename: req.Filename, Mode: req.Mode, Direction: "read", Outcome: err.Error(), TID: conn.LocalAddr().(*net.UDPAddr).Port})
		return
	}
	blocks := tftpBlocks(data)
	sent, bytes := 0, 0
	for i, blockData := range blocks {
		block := uint16(i + 1)
		if err := s.sendDataAndWait(conn, client, block, blockData); err != nil {
			s.addLog(TFTPLog{At: time.Now(), Filename: req.Filename, Mode: req.Mode, Direction: "read", Outcome: err.Error(), Blocks: sent, Bytes: bytes, TID: conn.LocalAddr().(*net.UDPAddr).Port})
			return
		}
		sent++
		bytes += len(blockData)
	}
	s.addLog(TFTPLog{At: time.Now(), Filename: req.Filename, Mode: req.Mode, Direction: "read", Outcome: "complete", Blocks: sent, Bytes: bytes, TID: conn.LocalAddr().(*net.UDPAddr).Port})
}

func tftpBlocks(data []byte) [][]byte {
	if len(data) == 0 {
		return [][]byte{nil}
	}
	blocks := make([][]byte, 0, len(data)/512+1)
	for offset := 0; offset < len(data); {
		end := offset + 512
		if end > len(data) {
			end = len(data)
		}
		blocks = append(blocks, append([]byte(nil), data[offset:end]...))
		offset = end
	}
	if len(data)%512 == 0 {
		blocks = append(blocks, nil)
	}
	return blocks
}

func (s *TFTPServer) sendDataAndWait(conn *net.UDPConn, client *net.UDPAddr, block uint16, data []byte) error {
	packet := tftpDataPacket(block, data)
	for attempt := 0; attempt < 4; attempt++ {
		if _, err := conn.WriteToUDP(packet, client); err != nil {
			return err
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 516)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				break
			}
			if from.IP.Equal(client.IP) && from.Port == client.Port && n >= 4 && binary.BigEndian.Uint16(buf[:2]) == tftpACK && binary.BigEndian.Uint16(buf[2:4]) == block {
				_ = conn.SetReadDeadline(time.Time{})
				return nil
			}
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	return errors.New("TFTP timeout waiting for ACK")
}

func (s *TFTPServer) serveWrite(conn *net.UDPConn, req TFTPRequest, client *net.UDPAddr) {
	if _, err := conn.WriteToUDP(tftpAckPacket(0), client); err != nil {
		return
	}
	var all []byte
	expected := uint16(1)
	blocks := 0
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 516)
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			s.addLog(TFTPLog{At: time.Now(), Filename: req.Filename, Mode: req.Mode, Direction: "write", Outcome: err.Error(), TID: conn.LocalAddr().(*net.UDPAddr).Port})
			return
		}
		if !from.IP.Equal(client.IP) || from.Port != client.Port || n < 4 {
			continue
		}
		if binary.BigEndian.Uint16(buf[:2]) != tftpDATA || binary.BigEndian.Uint16(buf[2:4]) != expected {
			continue
		}
		if len(all)+n-4 > 32*1024*1024 {
			_, _ = conn.WriteToUDP(tftpErrorPacket(tftpErrAccess, "file exceeds 32 MiB limit"), client)
			return
		}
		all = append(all, buf[4:n]...)
		_, _ = conn.WriteToUDP(tftpAckPacket(expected), client)
		blocks++
		if n-4 < 512 {
			break
		}
		expected++
	}
	_ = conn.SetReadDeadline(time.Time{})
	if err := s.writeFile(req.Filename, all); err != nil {
		_, _ = conn.WriteToUDP(tftpErrorPacket(tftpErrAccess, err.Error()), client)
		s.addLog(TFTPLog{At: time.Now(), Filename: req.Filename, Mode: req.Mode, Direction: "write", Outcome: err.Error(), Blocks: blocks, Bytes: len(all), TID: conn.LocalAddr().(*net.UDPAddr).Port})
		return
	}
	s.addLog(TFTPLog{At: time.Now(), Filename: req.Filename, Mode: req.Mode, Direction: "write", Outcome: "complete", Blocks: blocks, Bytes: len(all), TID: conn.LocalAddr().(*net.UDPAddr).Port})
}

func (s *TFTPServer) readFile(name string) ([]byte, error) {
	if err := validateTFTPName(name); err != nil {
		return nil, err
	}
	cfg := s.store.Snapshot()
	if value, ok := cfg.TFTP.Files[name]; ok {
		return []byte(value), nil
	}
	path := filepath.Join(uploadDir(s.optionsPath), name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.New("file not found")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("symlink refused")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > 32*1024*1024 {
		return nil, errors.New("file exceeds 32 MiB limit")
	}
	return os.ReadFile(path)
}

func (s *TFTPServer) writeFile(name string, data []byte) error {
	if err := validateTFTPName(name); err != nil {
		return err
	}
	dir := uploadDir(s.optionsPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink refused")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var _ = fmt.Sprintf
