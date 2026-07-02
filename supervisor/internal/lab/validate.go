package lab

import (
	"fmt"

	"github.com/rohanpunj/iolab/supervisor/internal/netmap"
)

// Validate enforces the invariants of lab.schema.json that are not expressible
// by struct decoding alone:
//
//   - version must equal 1
//   - id and name are non-empty
//   - node ids are unique and >= 0
//   - node kind is iol or vpcs
//   - iol nodes carry an image reference with a non-empty id
//   - ethernet/serial group counts are within 0..16
//   - ram, when set, is >= 32
//   - link ids are unique and >= 0
//   - a p2p link has exactly 2 endpoints; any link has >= 2
//   - every endpoint references an existing node
//   - every IOL endpoint interface string parses
//
// It returns the first violation encountered, or nil.
func (l *Lab) Validate() error {
	if l.Version != Version {
		return fmt.Errorf("version must be %d, got %d", Version, l.Version)
	}
	if l.ID == "" {
		return fmt.Errorf("lab id is required")
	}
	if l.Name == "" {
		return fmt.Errorf("lab name is required")
	}

	nodeByID := make(map[int]*Node, len(l.Nodes))
	for i := range l.Nodes {
		n := &l.Nodes[i]
		if n.ID < 0 {
			return fmt.Errorf("node %q: id must be >= 0", n.Name)
		}
		if _, dup := nodeByID[n.ID]; dup {
			return fmt.Errorf("duplicate node id %d", n.ID)
		}
		if n.Name == "" {
			return fmt.Errorf("node %d: name is required", n.ID)
		}
		switch n.Kind {
		case KindIOL:
			if n.Image == nil || n.Image.ID == "" {
				return fmt.Errorf("node %d (iol): image reference with id is required", n.ID)
			}
		case KindVPCS:
			// image is ignored for vpcs
		default:
			return fmt.Errorf("node %d: kind must be iol or vpcs, got %q", n.ID, n.Kind)
		}
		if n.Ethernet != nil && (*n.Ethernet < 0 || *n.Ethernet > 16) {
			return fmt.Errorf("node %d: ethernet groups must be 0..16, got %d", n.ID, *n.Ethernet)
		}
		if n.Serial != nil && (*n.Serial < 0 || *n.Serial > 16) {
			return fmt.Errorf("node %d: serial groups must be 0..16, got %d", n.ID, *n.Serial)
		}
		if n.RAM != 0 && n.RAM < 32 {
			return fmt.Errorf("node %d: ram must be >= 32, got %d", n.ID, n.RAM)
		}
		nodeByID[n.ID] = n
	}

	linkIDs := make(map[int]bool, len(l.Links))
	for _, link := range l.Links {
		if link.ID < 0 {
			return fmt.Errorf("link id must be >= 0")
		}
		if linkIDs[link.ID] {
			return fmt.Errorf("duplicate link id %d", link.ID)
		}
		linkIDs[link.ID] = true

		if len(link.Endpoints) < 2 {
			return fmt.Errorf("link %d: needs at least 2 endpoints", link.ID)
		}
		if link.EffectiveType() == LinkP2P && len(link.Endpoints) != 2 {
			return fmt.Errorf("link %d: p2p must have exactly 2 endpoints, got %d", link.ID, len(link.Endpoints))
		}

		for _, ep := range link.Endpoints {
			n, ok := nodeByID[ep.Node]
			if !ok {
				return fmt.Errorf("link %d: endpoint references unknown node %d", link.ID, ep.Node)
			}
			if ep.Interface == "" {
				return fmt.Errorf("link %d: endpoint on node %d has empty interface", link.ID, ep.Node)
			}
			// IOL interfaces must parse to adapter/port. VPCS uses ethN and is
			// validated leniently (a non-empty string is enough).
			if n.Kind == KindIOL {
				if _, err := netmap.ParseIface(ep.Interface); err != nil {
					return fmt.Errorf("link %d: %w", link.ID, err)
				}
			}
		}
	}
	return nil
}
