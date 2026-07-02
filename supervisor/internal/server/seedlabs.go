package server

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// seedLabFS carries the predefined starter topologies (2-routers, triangle,
// switch blocks, campus, CCNA/CCNP capstones) shipped inside the binary. They
// materialize into LabsDir on startup ONLY when the store holds no labs yet,
// so a user who deletes or edits them never has them forced back.
//
//go:embed seedlabs/*.json
var seedLabFS embed.FS

// seedLabs writes the embedded starter labs into LabsDir if (and only if) the
// store currently contains no lab documents. Failures are logged, never fatal:
// missing starter labs must not stop the supervisor.
func (s *Server) seedLabs() {
	dir := s.cfg.LabsDir
	if dir == "" {
		return
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				return // store already has labs — user territory now
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("seed labs: mkdir %s: %v", dir, err)
		return
	}
	seeds, err := fs.ReadDir(seedLabFS, "seedlabs")
	if err != nil {
		log.Printf("seed labs: %v", err)
		return
	}
	n := 0
	for _, e := range seeds {
		data, err := seedLabFS.ReadFile("seedlabs/" + e.Name())
		if err != nil {
			log.Printf("seed labs: read %s: %v", e.Name(), err)
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), data, 0o644); err != nil {
			log.Printf("seed labs: write %s: %v", e.Name(), err)
			continue
		}
		n++
	}
	if n > 0 {
		log.Printf("seeded %d starter labs into %s", n, dir)
	}
}
