package server

import "fmt"

// nvramFilename returns the NVRAM filename IOL writes in its working directory,
// e.g. "nvram_00003" for node id 3.
//
// ASSUMPTION (verify in P0): the exact filename/format. Kept here so both the
// Linux extractor and any future injector agree on one place to change.
func nvramFilename(id int) string {
	return fmt.Sprintf("nvram_%05d", id)
}
