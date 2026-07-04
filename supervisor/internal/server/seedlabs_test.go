package server

import (
	"io/fs"
	"strings"
	"testing"
)

// TestSeedLabsValidate sanity-checks the embedded YAML seed labs from the Go
// side (the supervisor has no YAML parser — deep structural validation lives in
// the frontend, which authored these from valid docs). Each seed must be
// non-empty, be named <id>.yml, and carry a top-level id matching its filename.
func TestSeedLabsValidate(t *testing.T) {
	seeds, err := fs.ReadDir(seedLabFS, "seedlabs")
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) == 0 {
		t.Fatal("no embedded seed labs")
	}
	for _, e := range seeds {
		if !strings.HasSuffix(e.Name(), ".yml") {
			t.Fatalf("%s: seed labs must be .yml", e.Name())
		}
		data, err := seedLabFS.ReadFile("seedlabs/" + e.Name())
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		if len(data) == 0 {
			t.Fatalf("%s: empty seed", e.Name())
		}
		id, ok := labDocID(string(data))
		if !ok {
			t.Fatalf("%s: no top-level id", e.Name())
		}
		if want := strings.TrimSuffix(e.Name(), ".yml"); id != want {
			t.Fatalf("%s: id %q must match filename base %q", e.Name(), id, want)
		}
	}
}
