package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"sync"
)

type Config struct {
	Admin        Admin                        `json:"admin"`
	ModuleParams map[string]map[string]string `json:"module_params"`
	RawArgs      map[string]string            `json:"raw_args"`
	ReconSubnet  string                       `json:"recon_subnet"`
}

type Admin struct {
	User     string `json:"user"`
	Salt     string `json:"salt"`
	PassHash string `json:"passhash"`
}

type Store struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func defaultConfig() Config {
	c := Config{ModuleParams: map[string]map[string]string{}, RawArgs: map[string]string{}}
	c.Admin.User = "admin"
	c.Admin.Salt = randHex(8)
	c.Admin.PassHash = hashPass(c.Admin.Salt, "pnet")
	return c
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
	if c.Admin.User == "" {
		c.Admin.User = "admin"
	}
	if c.Admin.Salt == "" || c.Admin.PassHash == "" {
		c.Admin.Salt = randHex(8)
		c.Admin.PassHash = hashPass(c.Admin.Salt, "pnet")
	}
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

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashPass(salt, pass string) string {
	h := sha256.Sum256([]byte(salt + pass))
	return hex.EncodeToString(h[:])
}
