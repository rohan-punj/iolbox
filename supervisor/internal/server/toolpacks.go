package server

import (
	"encoding/json"
	"log"

	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
	"github.com/rohanpunj/iolbox/supervisor/internal/tool"
)

// handleToolListPacks exposes validated pack metadata so the client can build
// its palette from the same registry used for tool-node validation.
func (s *Server) handleToolListPacks(_ json.RawMessage) (any, error) {
	result := protocol.ToolListPacksResult{
		Packs: make([]protocol.ToolPackInfo, 0, len(s.toolPacks)),
	}
	for _, pack := range s.toolPacks {
		groups := make([]string, len(pack.Manifest.Groups))
		copy(groups, pack.Manifest.Groups)
		modules := make([]protocol.ToolModuleInfo, 0, len(pack.Manifest.Modules))
		for _, module := range pack.Manifest.Modules {
			modules = append(modules, protocol.ToolModuleInfo{
				Key:   module.Key,
				Label: module.Label,
				Group: module.Group,
			})
		}
		result.Packs = append(result.Packs, protocol.ToolPackInfo{
			ID:        pack.ID,
			Name:      pack.Manifest.Name,
			Icon:      pack.Manifest.Icon,
			Transport: pack.Manifest.GUI.Transport,
			Groups:    groups,
			Modules:   modules,
		})
	}
	return result, nil
}

// toolPack centralizes registry lookup so load-time validation and endpoint
// startup use the same installed-pack identity and cannot drift apart.
func (s *Server) toolPack(id string) (tool.Pack, bool) {
	for _, pack := range s.toolPacks {
		if pack.ID == id {
			return pack, true
		}
	}
	return tool.Pack{}, false
}

// toolpacksLoad preserves successful packs when one manifest is malformed;
// the appliance must retain every usable palette entry while warning about
// the rejected entry.
func (s *Server) toolpacksLoad(dir string) {
	packs, err := tool.LoadPacks(dir)
	s.toolPacks = packs
	if err != nil {
		log.Printf("supervisor: warning: tool pack load: %v", err)
	}
}
