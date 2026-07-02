//go:build linux

package server

import (
	"os"
	"path/filepath"

	"github.com/rohanpunj/iolab/supervisor/internal/nvram"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// extractNVRAM reads the node's NVRAM file from its working directory and
// decodes the startup-config text. IOL writes NVRAM as a file named
// "nvram_<5-digit-id>" in its cwd.
//
// ASSUMPTION (verify in P0): the exact NVRAM filename IOL uses. We centralise
// it in nvramFilename so P0 can fix it in one place.
func (s *Server) extractNVRAM(ll *loadedLab, id int) (string, error) {
	path := filepath.Join(ll.workDir(id), nvramFilename(id))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // no NVRAM yet = empty config, not an error
		}
		return "", protocol.Errorf(protocol.CodeNvramCodecFailed, "read nvram node %d: %v", id, err)
	}
	cfg, err := nvram.Decode(data)
	if err != nil {
		return "", protocol.Errorf(protocol.CodeNvramCodecFailed, "decode nvram node %d: %v", id, err)
	}
	return cfg, nil
}
