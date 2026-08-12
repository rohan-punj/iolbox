package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Config is deliberately the pack's own configuration. The DHCP codec is a
// clean-room implementation of the sibling supervisor/extnet codec: this
// module cannot import an internal supervisor package, and netsvc needs
// configurable DNS/NTP/TFTP policy rather than NAT's fixed policy.
type Config struct {
	DHCP  DHCPConfig  `json:"dhcp"`
	DNS   DNSConfig   `json:"dns"`
	NTP   NTPConfig   `json:"ntp"`
	TFTP  TFTPConfig  `json:"tftp"`
	Ports PortsConfig `json:"ports"`
}

type PortsConfig struct {
	DNS  int `json:"dns"`
	DHCP int `json:"dhcp"`
	NTP  int `json:"ntp"`
	TFTP int `json:"tftp"`
}

type DHCPConfig struct {
	ServerIP      string            `json:"serverIP"`
	Pools         []DHCPPool        `json:"pools"`
	Reservations  map[string]string `json:"reservations"`
	DNSServers    []string          `json:"dnsServers"`
	NTPServers    []string          `json:"ntpServers"`
	TFTPName      string            `json:"tftpName"`
	TFTPAddresses []string          `json:"tftpAddresses"`
	LeaseSeconds  uint32            `json:"leaseSeconds"`
}

type DHCPPool struct {
	Subnet     string `json:"subnet"`
	RangeStart string `json:"rangeStart"`
	RangeEnd   string `json:"rangeEnd"`
	Router     string `json:"router"`
}

type DNSConfig struct {
	Zone    string      `json:"zone"`
	Records []DNSRecord `json:"records"`
}

type DNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   uint32 `json:"ttl"`
}

type NTPConfig struct {
	Stratum  uint8  `json:"stratum"`
	ServerIP string `json:"serverIP"`
}

type TFTPConfig struct {
	Files map[string]string `json:"files"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func defaultConfig() Config {
	return Config{
		DHCP: DHCPConfig{
			Pools:        []DHCPPool{{Subnet: "192.0.2.0/24", RangeStart: "192.0.2.100", RangeEnd: "192.0.2.200", Router: "192.0.2.1"}},
			Reservations: map[string]string{}, LeaseSeconds: 3600,
		},
		DNS:   DNSConfig{Zone: "lab."},
		NTP:   NTPConfig{Stratum: 3},
		TFTP:  TFTPConfig{Files: map[string]string{}},
		Ports: PortsConfig{DNS: 53, DHCP: 67, NTP: 123, TFTP: 69},
	}
}

func NewStore(path string) *Store { return &Store{path: path, cfg: defaultConfig()} }

func (s *Store) Load() error {
	if s.path == "" {
		return nil
	}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	cfg = normalizeConfig(cfg)
	s.mu.Lock()
	s.cfg = cfg
	s.mu.Unlock()
	return nil
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

func (s *Store) Save(cfg Config) error {
	cfg = normalizeConfig(cfg)
	s.mu.Lock()
	s.cfg = cloneConfig(cfg)
	s.mu.Unlock()
	if s.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func normalizeConfig(cfg Config) Config {
	d := defaultConfig()
	if cfg.Ports.DNS == 0 {
		cfg.Ports.DNS = d.Ports.DNS
	}
	if cfg.Ports.DHCP == 0 {
		cfg.Ports.DHCP = d.Ports.DHCP
	}
	if cfg.Ports.NTP == 0 {
		cfg.Ports.NTP = d.Ports.NTP
	}
	if cfg.Ports.TFTP == 0 {
		cfg.Ports.TFTP = d.Ports.TFTP
	}
	if cfg.DHCP.LeaseSeconds == 0 {
		cfg.DHCP.LeaseSeconds = d.DHCP.LeaseSeconds
	}
	if len(cfg.DHCP.Pools) == 0 {
		cfg.DHCP.Pools = d.DHCP.Pools
	}
	if cfg.DHCP.Reservations == nil {
		cfg.DHCP.Reservations = map[string]string{}
	}
	if cfg.DNS.Zone == "" {
		cfg.DNS.Zone = d.DNS.Zone
	}
	if cfg.NTP.Stratum == 0 || cfg.NTP.Stratum > 16 {
		cfg.NTP.Stratum = d.NTP.Stratum
	}
	if cfg.TFTP.Files == nil {
		cfg.TFTP.Files = map[string]string{}
	}
	return cfg
}

func cloneConfig(cfg Config) Config {
	cfg.DHCP.Pools = append([]DHCPPool(nil), cfg.DHCP.Pools...)
	cfg.DHCP.DNSServers = append([]string(nil), cfg.DHCP.DNSServers...)
	cfg.DHCP.NTPServers = append([]string(nil), cfg.DHCP.NTPServers...)
	cfg.DHCP.TFTPAddresses = append([]string(nil), cfg.DHCP.TFTPAddresses...)
	cfg.DHCP.Reservations = cloneStringMap(cfg.DHCP.Reservations)
	cfg.DNS.Records = append([]DNSRecord(nil), cfg.DNS.Records...)
	cfg.TFTP.Files = cloneStringMap(cfg.TFTP.Files)
	return cfg
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
