package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SocketManager struct {
	mu      sync.Mutex
	nextID  int
	tcp     map[int]net.Listener
	tcpConn map[int]net.Conn
	tcpPort map[int]int
	udp     map[int]*net.UDPConn
	udpEcho map[int]bool
}

func NewSocketManager() *SocketManager {
	return &SocketManager{nextID: 1, tcp: map[int]net.Listener{}, tcpConn: map[int]net.Conn{}, tcpPort: map[int]int{}, udp: map[int]*net.UDPConn{}, udpEcho: map[int]bool{}}
}

func (m *SocketManager) ListenTCP(port int, echo bool) (int, error) {
	ln, err := net.Listen("tcp4", ":"+strconv.Itoa(port))
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.tcp[id] = ln
	m.mu.Unlock()
	go acceptTCP(id, ln, echo)
	return id, nil
}

func acceptTCP(id int, ln net.Listener, echo bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			if !echo {
				_, _ = conn.Write([]byte(fmt.Sprintf("PC TCP socket %d\r\n", id)))
				return
			}
			buf := make([]byte, 2048)
			for {
				n, readErr := conn.Read(buf)
				if readErr != nil {
					return
				}
				_, _ = conn.Write(buf[:n])
			}
		}()
	}
}

func (m *SocketManager) ListenUDP(port int, echo bool) (int, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.udp[id] = conn
	m.udpEcho[id] = echo
	m.mu.Unlock()
	go serveUDP(conn, echo)
	return id, nil
}

func serveUDP(conn *net.UDPConn, echo bool) {
	if !echo {
		return
	}
	buf := make([]byte, 64*1024)
	for {
		n, peer, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if _, err := conn.WriteToUDP(buf[:n], peer); err != nil {
			return
		}
	}
}

func (m *SocketManager) ConnectTCP(host string, port int) (int, error) {
	conn, err := net.Dial("tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	id := m.nextID
	m.nextID++
	m.tcpConn[id] = conn
	m.tcpPort[id] = port
	m.mu.Unlock()
	return id, nil
}

func (m *SocketManager) SendTCP(id int, message string) error {
	m.mu.Lock()
	conn, ok := m.tcpConn[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("socket %d not found", id)
	}
	_, err := conn.Write([]byte(message))
	return err
}

func (m *SocketManager) ClosePort(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ln := range m.tcp {
		if strings.HasSuffix(ln.Addr().String(), ":"+strconv.Itoa(port)) {
			delete(m.tcp, id)
			return ln.Close()
		}
	}
	for id, conn := range m.udp {
		if conn.LocalAddr().(*net.UDPAddr).Port == port {
			delete(m.udp, id)
			delete(m.udpEcho, id)
			return conn.Close()
		}
	}
	for id, conn := range m.tcpConn {
		if m.tcpPort[id] == port {
			delete(m.tcpConn, id)
			delete(m.tcpPort, id)
			return conn.Close()
		}
	}
	return fmt.Errorf("port %d not found", port)
}

func (m *SocketManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ln := range m.tcp {
		_ = ln.Close()
		delete(m.tcp, id)
	}
	for id, conn := range m.tcpConn {
		_ = conn.Close()
		delete(m.tcpConn, id)
		delete(m.tcpPort, id)
	}
	for id, conn := range m.udp {
		_ = conn.Close()
		delete(m.udp, id)
		delete(m.udpEcho, id)
	}
}

func (m *SocketManager) Show() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := []string{"ID  TYPE"}
	for id := range m.tcp {
		rows = append(rows, fmt.Sprintf("%d  tcp", id))
	}
	for id := range m.tcpConn {
		rows = append(rows, fmt.Sprintf("%d  tcp-connection", id))
	}
	for id := range m.udp {
		rows = append(rows, fmt.Sprintf("%d  udp", id))
	}
	return strings.Join(rows, "\n")
}

func commandTCP(app *App, args []string) string {
	if len(args) >= 3 && args[0] == "connect" {
		if len(args) != 3 && len(args) != 5 || len(args) == 5 && args[3] != "-m" {
			return malformed("tcp connect <host> <port> [-m <msg>]")
		}
		port, err := parsePort(args[2])
		if err != nil {
			return malformed("tcp connect <host> <port> [-m <msg>]")
		}
		started := time.Now()
		id, err := app.socks.ConnectTCP(args[1], port)
		if err != nil {
			return "% tcp: " + err.Error()
		}
		if len(args) == 5 {
			_ = app.socks.SendTCP(id, args[4])
		}
		return fmt.Sprintf("TCP connection %d opened; handshake %.1f ms.", id, float64(time.Since(started).Microseconds())/1000)
	}
	if len(args) >= 2 && args[0] == "listen" {
		if len(args) > 3 || len(args) == 3 && args[2] != "-e" {
			return malformed("tcp listen <port> [-e]")
		}
		port, err := parsePort(args[1])
		if err != nil {
			return malformed("tcp listen <port> [-e]")
		}
		id, err := app.socks.ListenTCP(port, len(args) == 3)
		if err != nil {
			return "% tcp: " + err.Error()
		}
		return fmt.Sprintf("TCP listener %d on %d", id, port)
	}
	if len(args) == 2 && args[0] == "close" {
		port, err := parsePort(args[1])
		if err != nil {
			return malformed("tcp close <port>")
		}
		if err := app.socks.ClosePort(port); err != nil {
			return "% tcp: " + err.Error()
		}
		return "TCP port closed."
	}
	return malformed("tcp connect <host> <port> [-m <msg>] | tcp listen <port> [-e] | tcp close <port>")
}

func commandUDP(app *App, args []string) string {
	if len(args) >= 4 && args[0] == "send" {
		port, err := parsePort(args[2])
		if err != nil {
			return malformed("udp send <host> <port> <msg>")
		}
		conn, err := net.Dial("udp4", net.JoinHostPort(args[1], strconv.Itoa(port)))
		if err != nil {
			return "% udp: " + err.Error()
		}
		defer conn.Close()
		if _, err := conn.Write([]byte(strings.Join(args[3:], " "))); err != nil {
			return "% udp: " + err.Error()
		}
		return "UDP datagram sent."
	}
	if len(args) >= 2 && args[0] == "listen" {
		if len(args) > 3 || len(args) == 3 && args[2] != "-e" {
			return malformed("udp listen <port> [-e]")
		}
		port, err := parsePort(args[1])
		if err != nil {
			return malformed("udp listen <port> [-e]")
		}
		id, err := app.socks.ListenUDP(port, len(args) == 3)
		if err != nil {
			return "% udp: " + err.Error()
		}
		return fmt.Sprintf("UDP listener %d on %d", id, port)
	}
	if len(args) == 2 && args[0] == "close" {
		port, err := parsePort(args[1])
		if err != nil {
			return malformed("udp close <port>")
		}
		if err := app.socks.ClosePort(port); err != nil {
			return "% udp: " + err.Error()
		}
		return "UDP port closed."
	}
	return malformed("udp send <host> <port> <msg> | udp listen <port> [-e] | udp close <port>")
}
