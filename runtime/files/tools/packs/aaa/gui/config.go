package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Client struct {
	Subnet string `json:"subnet"`
	Secret string `json:"secret"`
}

type User struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Service       string `json:"service"`
	TacacsService string `json:"tacacsService"`
	PrivLvl       int    `json:"privLvl"`
}

type Config struct {
	SharedSecret string   `json:"sharedSecret"`
	TacacsKey    string   `json:"tacacsKey"`
	Clients      []Client `json:"clients"`
	Users        []User   `json:"users"`
	Protocol     string   `json:"protocol"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func defaultConfig() Config {
	return Config{SharedSecret: "labsecret", Protocol: "both", Users: []User{}}
}

func NewStore(path string) *Store {
	return &Store{path: path, cfg: defaultConfig()}
}

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
	// The supervisor writes a bare "{}" for a node with no saved pack options
	// yet (endpointOptionsPayload) — that decodes to an all-zero-value Config,
	// which must not silently wipe out the shared-secret default the way a
	// genuine empty-string save from the settings form should not either.
	// Mirrors webserver/gui/config.go's own zero-value guards.
	if cfg.SharedSecret == "" {
		cfg.SharedSecret = defaultConfig().SharedSecret
	}
	if cfg.Protocol != "radius" && cfg.Protocol != "tacacs" && cfg.Protocol != "both" {
		cfg.Protocol = "both"
	}
	for i := range cfg.Users {
		if cfg.Users[i].TacacsService == "" {
			cfg.Users[i].TacacsService = "shell"
		}
	}
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

func (s *Store) Set(cfg Config) {
	s.mu.Lock()
	s.cfg = cloneConfig(cfg)
	s.mu.Unlock()
}

func (s *Store) Update(fn func(*Config)) error {
	s.mu.Lock()
	fn(&s.cfg)
	cfg := cloneConfig(s.cfg)
	s.mu.Unlock()
	return s.save(cfg)
}

func (s *Store) Save(cfg Config) error {
	s.Set(cfg)
	return s.save(cfg)
}

func (s *Store) save(cfg Config) error {
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

func cloneConfig(cfg Config) Config {
	cfg.Clients = append([]Client(nil), cfg.Clients...)
	cfg.Users = append([]User(nil), cfg.Users...)
	return cfg
}
