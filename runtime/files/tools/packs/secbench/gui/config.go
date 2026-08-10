package main

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

type Config struct {
	ModuleParams map[string]map[string]string `json:"module_params"`
	RawArgs      map[string]string            `json:"raw_args"`
	ReconSubnet  string                       `json:"recon_subnet"`
}

type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func defaultConfig() Config {
	return Config{ModuleParams: map[string]map[string]string{}, RawArgs: map[string]string{}}
}

func NewStore(path string) *Store {
	s := &Store{path: path, cfg: defaultConfig()}
	data, err := os.ReadFile(path)
	if err == nil && len(strings.TrimSpace(string(data))) > 0 {
		var c Config
		if json.Unmarshal(data, &c) == nil {
			s.cfg = mergeDefaults(c)
		} else {
			logf("config: %s is not valid JSON, using defaults", path)
		}
	}
	return s
}

func mergeDefaults(c Config) Config {
	if c.ModuleParams == nil {
		c.ModuleParams = map[string]map[string]string{}
	}
	if c.RawArgs == nil {
		c.RawArgs = map[string]string{}
	}
	return c
}

func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *Store) Update(fn func(c *Config)) error {
	s.mu.Lock()
	fn(&s.cfg)
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
