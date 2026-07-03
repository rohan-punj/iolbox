package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// qmp.go — a minimal QMP (QEMU Machine Protocol) client, just enough to send
// `system_powerdown` (ACPI soft-off) and `quit`. QMP is line-delimited JSON:
// on connect the server sends a greeting, the client MUST send
// {"execute":"qmp_capabilities"} to leave negotiation mode, then commands.

// qmpDial connects to the QMP TCP socket and completes the capabilities
// handshake, returning a ready connection + reader.
func qmpDial(addr string) (net.Conn, *bufio.Reader, error) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)

	// Read the greeting line ({"QMP": {...}}).
	if _, err := r.ReadBytes('\n'); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("qmp greeting read: %w", err)
	}
	// Negotiate out of capabilities mode.
	if err := qmpSend(conn, r, map[string]any{"execute": "qmp_capabilities"}); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, r, nil
}

// qmpSend writes one command and reads reply lines until a {"return"} or
// {"error"} object is seen (skipping async {"event"} lines).
func qmpSend(conn net.Conn, r *bufio.Reader, cmd map[string]any) error {
	buf, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(buf, '\n')); err != nil {
		return err
	}
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return fmt.Errorf("qmp reply read: %w", err)
		}
		var msg map[string]json.RawMessage
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		if _, ok := msg["event"]; ok {
			continue // async event, keep reading
		}
		if _, ok := msg["error"]; ok {
			return fmt.Errorf("qmp error: %s", string(line))
		}
		if _, ok := msg["return"]; ok {
			return nil
		}
	}
}

// qmpPowerdown sends an ACPI soft-power-off (the guest kernel sees a power
// button press and shuts down cleanly).
func qmpPowerdown(addr string) error {
	conn, r, err := qmpDial(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return qmpSend(conn, r, map[string]any{"execute": "system_powerdown"})
}

// qmpQuit tells the qemu process itself to exit immediately (no guest ACPI).
func qmpQuit(addr string) error {
	conn, r, err := qmpDial(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	return qmpSend(conn, r, map[string]any{"execute": "quit"})
}
