package server

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
)

func TestSeedLabsValidate(t *testing.T) {
	seeds, err := fs.ReadDir(seedLabFS, "seedlabs")
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) == 0 {
		t.Fatal("no embedded seed labs")
	}
	for _, e := range seeds {
		data, err := seedLabFS.ReadFile("seedlabs/" + e.Name())
		if err != nil {
			t.Fatalf("%s: %v", e.Name(), err)
		}
		var doc lab.Lab
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: unmarshal: %v", e.Name(), err)
		}
		if err := doc.Validate(); err != nil {
			t.Fatalf("%s: validate: %v", e.Name(), err)
		}
	}
}
