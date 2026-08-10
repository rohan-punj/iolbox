package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	ListenPort int               `json:"listenPort"`
	IndexHTML  string            `json:"indexHTML"`
	ExtraPaths map[string]string `json:"extraPaths,omitempty"`
}

func defaultConfig() Config {
	return Config{ListenPort: 8080, IndexHTML: "<h1>IOLbox lab web server</h1>\n", ExtraPaths: map[string]string{}}
}

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
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
	if cfg.ListenPort == 0 {
		cfg.ListenPort = 8080
	}
	if cfg.IndexHTML == "" {
		cfg.IndexHTML = defaultConfig().IndexHTML
	}
	if cfg.ExtraPaths == nil {
		cfg.ExtraPaths = map[string]string{}
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

func (s *Store) Set(cfg Config) {
	s.mu.Lock()
	s.cfg = cloneConfig(cfg)
	s.mu.Unlock()
}

func (s *Store) Save(cfg Config) error {
	s.Set(cfg)
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
	cfg.ExtraPaths = make(map[string]string, len(cfg.ExtraPaths))
	for path, body := range cfg.ExtraPaths {
		cfg.ExtraPaths[path] = body
	}
	return cfg
}
