package main

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

type Address struct {
	IP      string
	Prefix  int
	Gateway string
}

type Lease struct {
	SubnetMask   string
	ServerID     string
	Router       string
	DNS          []string
	LeaseSeconds uint32
	TFTPName     string
	TFTPServers  []string
	Options      map[byte][]byte
}

type RuntimeState struct {
	mu      sync.RWMutex
	store   *Store
	addr    Address
	lease   *Lease
	history []string
}

func NewRuntimeState(store *Store) *RuntimeState {
	return &RuntimeState{store: store, history: append([]string(nil), store.Snapshot().PC.SavedCommands...)}
}

func (s *RuntimeState) Remember(command string) {
	s.mu.Lock()
	if command != "" {
		s.history = append(s.history, command)
		if len(s.history) > 64 {
			s.history = s.history[len(s.history)-64:]
		}
	}
	s.mu.Unlock()
}

// History returns the remembered commands oldest-first, for the CLI's
// up/down arrow recall. The caller owns the returned slice.
func (s *RuntimeState) History() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.history...)
}

func (s *RuntimeState) SetAddress(ip string, prefix int, gateway string) error {
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil || parsed.To4() == nil {
		return fmt.Errorf("expected an IPv4 address")
	}
	if prefix < 1 || prefix > 32 {
		return fmt.Errorf("prefix must be 1..32")
	}
	if gateway != "" && (net.ParseIP(gateway) == nil || net.ParseIP(gateway).To4() == nil) {
		return fmt.Errorf("expected an IPv4 gateway")
	}
	if gateway != "" {
		_, network, _ := net.ParseCIDR(fmt.Sprintf("%s/%d", parsed.To4(), prefix))
		if !network.Contains(net.ParseIP(gateway).To4()) {
			return fmt.Errorf("gateway is outside the configured prefix")
		}
	}
	s.mu.Lock()
	s.addr = Address{IP: parsed.To4().String(), Prefix: prefix, Gateway: gateway}
	s.mu.Unlock()
	if err := runIP("addr", "replace", fmt.Sprintf("%s/%d", parsed.To4(), prefix), "dev", "eth1"); err != nil && hasLabIface() {
		return err
	}
	if gateway != "" {
		_ = runIP("route", "replace", "default", "via", gateway, "dev", "eth1")
	}
	return nil
}

func (s *RuntimeState) ClearAddress() error {
	s.mu.Lock()
	s.addr = Address{}
	s.mu.Unlock()
	_ = runIP("addr", "flush", "dev", "eth1")
	_ = runIP("route", "del", "default", "dev", "eth1")
	return nil
}

func (s *RuntimeState) SetLease(lease Lease) {
	s.mu.Lock()
	s.lease = &lease
	s.mu.Unlock()
}

func (s *RuntimeState) ClearLease() { s.mu.Lock(); s.lease = nil; s.mu.Unlock() }

func (s *RuntimeState) Snapshot() (Address, *Lease, Config) {
	s.mu.RLock()
	addr, lease := s.addr, s.lease
	if lease != nil {
		copyLease := *lease
		copyLease.DNS = append([]string(nil), lease.DNS...)
		copyLease.TFTPServers = append([]string(nil), lease.TFTPServers...)
		copyLease.Options = cloneDHCPOptions(lease.Options)
		lease = &copyLease
	}
	s.mu.RUnlock()
	return addr, lease, s.store.Snapshot()
}

func (s *RuntimeState) Save() error {
	addr, _, cfg := s.Snapshot()
	s.mu.RLock()
	cfg.PC.SavedCommands = append([]string(nil), s.history...)
	s.mu.RUnlock()
	if addr.IP != "" {
		command := fmt.Sprintf("ip %s/%d", addr.IP, addr.Prefix)
		if addr.Gateway != "" {
			command += " " + addr.Gateway
		}
		found := false
		for _, saved := range cfg.PC.SavedCommands {
			if saved == command {
				found = true
				break
			}
		}
		if !found {
			cfg.PC.SavedCommands = append(cfg.PC.SavedCommands, command)
		}
	}
	return s.store.Save(cfg)
}

func (s *RuntimeState) Reset() {
	_ = s.ClearAddress()
	_ = runIP("neigh", "flush", "dev", "eth1")
	s.mu.Lock()
	s.lease = nil
	s.mu.Unlock()
}

func (s *RuntimeState) ShowIP() string {
	addr, lease, cfg := s.Snapshot()
	iface, _ := net.InterfaceByName("eth1")
	mac, mtu := "unknown", 0
	if iface != nil {
		mac, mtu = iface.HardwareAddr.String(), iface.MTU
	}
	if addr.IP == "" {
		if lease != nil && lease.ServerID != "" {
			return fmt.Sprintf("eth1: DHCP lease from %s mac %s mtu %d", lease.ServerID, mac, mtu)
		}
		return fmt.Sprintf("eth1: unaddressed mac %s mtu %d (DHCP=%t)", mac, mtu, cfg.PC.DHCP)
	}
	line := fmt.Sprintf("eth1: %s/%d", addr.IP, addr.Prefix)
	if addr.Gateway != "" {
		line += " gateway " + addr.Gateway
	}
	if cfg.PC.DHCP {
		line += " (DHCP enabled)"
	}
	line += fmt.Sprintf(" mac %s mtu %d", mac, mtu)
	if lease != nil {
		line += fmt.Sprintf(" lease-server %s dns %s", lease.ServerID, strings.Join(lease.DNS, ","))
	}
	return line
}
