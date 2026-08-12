package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"
)

const ntpEpochOffset int64 = 2208988800

type NTPLog struct {
	At                                        time.Time
	Source, ClientTransmit, Receive, Transmit string
	Offset, Delay                             time.Duration
	Mode                                      byte
	Rejected                                  bool
}

type NTPServer struct {
	store *Store
	mu    sync.RWMutex
	logs  []NTPLog
}

func NewNTPServer(store *Store) *NTPServer { return &NTPServer{store: store} }
func (s *NTPServer) Logs() []NTPLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]NTPLog(nil), s.logs...)
}
func (s *NTPServer) addLog(row NTPLog) {
	s.mu.Lock()
	s.logs = append([]NTPLog{row}, s.logs...)
	if len(s.logs) > 100 {
		s.logs = s.logs[:100]
	}
	s.mu.Unlock()
}

func encodeNTPTimestamp(t time.Time) [8]byte {
	var out [8]byte
	seconds := uint64(t.Unix() + ntpEpochOffset)
	fraction := uint64(t.Nanosecond()) * (uint64(1) << 32) / 1_000_000_000
	binary.BigEndian.PutUint32(out[0:4], uint32(seconds))
	binary.BigEndian.PutUint32(out[4:8], uint32(fraction))
	return out
}

func decodeNTPTimestamp(b []byte) (time.Time, error) {
	if len(b) < 8 {
		return time.Time{}, fmt.Errorf("NTP timestamp needs 8 bytes")
	}
	seconds := int64(binary.BigEndian.Uint32(b[0:4]))
	fraction := int64(binary.BigEndian.Uint32(b[4:8]))
	nanos := fraction * 1_000_000_000 / (int64(1) << 32)
	return time.Unix(seconds-ntpEpochOffset, nanos), nil
}

func encodeNTPResponse(request []byte, receive, transmit time.Time, stratum uint8, referenceIP string) ([]byte, error) {
	if len(request) < 48 {
		return nil, fmt.Errorf("NTP request needs 48 bytes")
	}
	if request[0]&7 != 3 {
		return nil, fmt.Errorf("NTP request mode %d is not client mode", request[0]&7)
	}
	version := (request[0] >> 3) & 7
	if version == 0 {
		version = 4
	}
	b := make([]byte, 48)
	b[0] = version<<3 | 4
	b[1] = stratum
	b[2] = request[2]
	precision := int8(-20)
	b[3] = byte(precision)
	// Root delay and dispersion are 16.16 fixed point values. A local lab
	// server has no upstream measurement, so use zero delay and one second of
	// dispersion; both are wire-compatible and deterministic.
	binary.BigEndian.PutUint32(b[8:12], 1<<16)
	if stratum == 1 {
		copy(b[12:16], []byte("LOCL"))
	} else if ip := net.ParseIP(referenceIP).To4(); ip != nil {
		copy(b[12:16], ip)
	}
	ref := encodeNTPTimestamp(receive)
	copy(b[16:24], ref[:])
	copy(b[24:32], request[40:48])
	recv := encodeNTPTimestamp(receive)
	copy(b[32:40], recv[:])
	tx := encodeNTPTimestamp(transmit)
	copy(b[40:48], tx[:])
	return b, nil
}

func (s *NTPServer) handlePacket(conn *net.UDPConn, data []byte, addr *net.UDPAddr) {
	now := time.Now()
	mode := byte(0)
	if len(data) > 0 {
		mode = data[0] & 7
	}
	if len(data) < 48 || mode != 3 {
		s.addLog(NTPLog{At: now, Source: addr.String(), Mode: mode, Rejected: true})
		return
	}
	receive := now
	transmit := time.Now()
	cfg := s.store.Snapshot().NTP
	if cfg.Stratum == 0 {
		cfg.Stratum = 3
	}
	referenceIP := cfg.ServerIP
	if referenceIP == "" {
		if ip := localIPv4(""); ip != nil {
			referenceIP = ip.String()
		}
	}
	response, err := encodeNTPResponse(data, receive, transmit, cfg.Stratum, referenceIP)
	if err != nil {
		s.addLog(NTPLog{At: now, Source: addr.String(), Mode: mode, Rejected: true})
		return
	}
	clientTime, _ := decodeNTPTimestamp(data[40:48])
	clientReceive := time.Now()
	// NTP's client-side equations: offset=((T2-T1)+(T3-T4))/2 and
	// delay=(T4-T1)-(T3-T2). Keeping these values in the GUI makes a bad
	// Originate echo visible immediately instead of hiding it behind a log.
	offset := (receive.Sub(clientTime) + transmit.Sub(clientReceive)) / 2
	delay := clientReceive.Sub(clientTime) - transmit.Sub(receive)
	s.addLog(NTPLog{At: now, Source: addr.String(), Mode: mode, ClientTransmit: clientTime.Format(time.RFC3339Nano), Receive: receive.Format(time.RFC3339Nano), Transmit: transmit.Format(time.RFC3339Nano), Offset: offset, Delay: delay})
	_, _ = conn.WriteToUDP(response, addr)
}
