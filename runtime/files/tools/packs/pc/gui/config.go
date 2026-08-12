package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type PCState struct {
	DHCP          bool     `json:"dhcp"`
	SavedCommands []string `json:"savedCommands"`
}

type Config struct {
	PC  PCState `json:"pc"`
	Rev int64   `json:"rev"`
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func defaultConfig() Config { return Config{PC: PCState{SavedCommands: []string{}}} }

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
	if cfg.PC.SavedCommands == nil {
		cfg.PC.SavedCommands = []string{}
	}
	s.mu.Lock()
	s.cfg = cloneConfig(cfg)
	s.mu.Unlock()
	return nil
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneConfig(s.cfg)
}

func (s *Store) Save(cfg Config) error {
	cfg = cloneConfig(cfg)
	if cfg.PC.SavedCommands == nil {
		cfg.PC.SavedCommands = []string{}
	}
	s.mu.Lock()
	if cfg.Rev <= s.cfg.Rev {
		cfg.Rev = s.cfg.Rev + 1
	}
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

func cloneConfig(cfg Config) Config {
	cfg.PC.SavedCommands = append([]string(nil), cfg.PC.SavedCommands...)
	if cfg.PC.SavedCommands == nil {
		cfg.PC.SavedCommands = []string{}
	}
	return cfg
}
