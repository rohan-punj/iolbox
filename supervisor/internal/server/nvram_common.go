package server

import (
	"fmt"

	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
)

// nvramFilename returns the NVRAM filename IOL reads/writes in its cwd for the
// given lab node id, e.g. "nvram_00002" for node id 1 (IOL instance 2). IOL
// names the file after its *instance* id, so this maps through
// netmap.InstanceID to stay in sync with the argv positional and NETMAP node
// id. Both the injector and the extractor call this one helper.
func nvramFilename(nodeID int) string {
	return fmt.Sprintf("nvram_%05d", netmap.InstanceID(nodeID))
}
