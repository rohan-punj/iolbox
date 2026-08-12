package main

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

const (
	maxFlowPPS   = 10000
	maxFlowBytes = 1500
	maxFlows     = 4
)

type FlowSpec struct {
	Protocol, Host            string
	Port, PPS, Bytes, Seconds int
}
type flow struct {
	id   int
	stop chan struct{}
}
type FlowManager struct {
	mu    sync.Mutex
	next  int
	flows map[int]flow
}

func NewFlowManager() *FlowManager { return &FlowManager{next: 1, flows: map[int]flow{}} }

func (m *FlowManager) Start(spec FlowSpec) (int, error) {
	if spec.Protocol != "udp" && spec.Protocol != "tcp" {
		return 0, fmt.Errorf("protocol must be tcp or udp")
	}
	if spec.PPS < 1 || spec.PPS > maxFlowPPS {
		return 0, fmt.Errorf("pps must be 1..%d", maxFlowPPS)
	}
	if spec.Bytes < 1 || spec.Bytes > maxFlowBytes {
		return 0, fmt.Errorf("bytes must be 1..%d", maxFlowBytes)
	}
	if spec.Seconds < 1 || spec.Seconds > 3600 {
		return 0, fmt.Errorf("seconds must be 1..3600")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.flows) >= maxFlows {
		return 0, fmt.Errorf("maximum %d active flows reached", maxFlows)
	}
	id := m.next
	m.next++
	f := flow{id: id, stop: make(chan struct{})}
	m.flows[id] = f
	go runFlow(spec, f.stop)
	return id, nil
}

func (m *FlowManager) Stop(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.flows[id]
	if !ok {
		return fmt.Errorf("flow %d not found", id)
	}
	close(f.stop)
	delete(m.flows, id)
	return nil
}
func (m *FlowManager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, f := range m.flows {
		close(f.stop)
		delete(m.flows, id)
	}
}
func (m *FlowManager) Show() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprintf("%d active flow(s), caps %d pps / %d bytes / %d flows", len(m.flows), maxFlowPPS, maxFlowBytes, maxFlows)
}

func runFlow(spec FlowSpec, stop <-chan struct{}) {
	interval := time.Second / time.Duration(spec.PPS)
	deadline := time.NewTimer(time.Duration(spec.Seconds) * time.Second)
	defer deadline.Stop()
	payload := make([]byte, spec.Bytes)
	for {
		select {
		case <-stop:
			return
		case <-deadline.C:
			return
		default:
		}
		if spec.Protocol == "udp" {
			conn, err := net.Dial("udp4", net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port)))
			if err == nil {
				_, _ = conn.Write(payload)
				_ = conn.Close()
			}
		} else if conn, err := net.DialTimeout("tcp4", net.JoinHostPort(spec.Host, strconv.Itoa(spec.Port)), tcpDialTimeout); err == nil {
			_, _ = conn.Write(payload)
			_ = conn.Close()
		}
		time.Sleep(interval)
	}
}

func commandFlow(app *App, args []string) string {
	if len(args) == 1 && args[0] == "show" {
		return app.flows.Show()
	}
	if len(args) == 1 && args[0] == "stop" {
		app.flows.StopAll()
		return "All flows stopped."
	}
	if len(args) == 2 && args[0] == "stop" {
		id, err := strconv.Atoi(args[1])
		if err != nil {
			return malformed("flow stop [<id>]")
		}
		if err := app.flows.Stop(id); err != nil {
			return "% flow: " + err.Error()
		}
		return "Flow stopped."
	}
	if len(args) >= 4 && args[0] == "start" {
		pps, e1 := strconv.Atoi(args[2])
		bytes, e2 := strconv.Atoi(args[3])
		if e1 != nil || e2 != nil {
			return malformed("flow start <dst> <pps> <bytes> [-p udp|tcp] [-d <port>]")
		}
		protocol, port := "udp", 9
		for i := 4; i < len(args); i += 2 {
			if i+1 >= len(args) || (args[i] != "-p" && args[i] != "-d") {
				return malformed("flow start <dst> <pps> <bytes> [-p udp|tcp] [-d <port>]")
			}
			if args[i] == "-p" {
				if args[i+1] != "udp" && args[i+1] != "tcp" {
					return malformed("flow start <dst> <pps> <bytes> [-p udp|tcp] [-d <port>]")
				}
				protocol = args[i+1]
			} else {
				parsed, err := parsePort(args[i+1])
				if err != nil {
					return malformed("flow start <dst> <pps> <bytes> [-p udp|tcp] [-d <port>]")
				}
				port = parsed
			}
		}
		id, err := app.flows.Start(FlowSpec{Protocol: protocol, Host: args[1], Port: port, PPS: pps, Bytes: bytes, Seconds: 3600})
		if err != nil {
			return "% flow: " + err.Error()
		}
		return fmt.Sprintf("Flow %d started.", id)
	}
	return malformed("flow start <dst> <pps> <bytes> [-p udp|tcp] [-d <port>] | flow stop [<id>] | flow show")
}
