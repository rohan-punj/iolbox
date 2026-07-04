//go:build linux

package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/painter"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

// painterCollect scrapes the requested protocol's `show` output from every
// targeted RUNNING IOL node, parses it (internal/painter), and maps it into the
// protocol result shape the frontend consumes. Non-running / no-data nodes get
// an explicit empty entry with a Hint — never fabricated data.
func (s *Server) painterCollect(ctx context.Context, ll *loadedLab, args protocol.PainterArgs) (protocol.PainterResult, error) {
	proto := strings.ToLower(strings.TrimSpace(args.Proto))
	res := protocol.PainterResult{Proto: proto, Dest: args.Dest, Nodes: []protocol.PainterNode{}}
	if proto == "stp" {
		res.VLAN = args.VLAN
	}

	ids := args.Nodes
	if ids == nil {
		// Auto-query ALL L2 bridges in the lab: every running IOL node (IOL
		// nodes can be L2 or L3 — a non-STP/L3 node just reports empty+hint
		// below, it is never crowned).
		for _, n := range ll.doc.Nodes {
			if n.Kind == lab.KindIOL {
				ids = append(ids, n.ID)
			}
		}
	}

	// Gather every node CONCURRENTLY, then return the assembled result in ONE
	// pass so the frontend paints the whole topology collectively instead of a
	// slow node-by-node dribble. Each node has its OWN console hub, so scraping
	// them in parallel is safe (no cross-node turn contention) and collapses
	// N serial console scrapes (~N seconds) into ~one scrape of wall-clock.
	// Results go into a pre-sized slice by index so order is preserved without
	// locking; a bounded worker count stops a huge lab from opening hundreds of
	// console sessions at once.
	nodes := make([]protocol.PainterNode, len(ids))
	const maxConcurrent = 16
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, id := range ids {
		pn := protocol.PainterNode{Node: id}

		// Only IOL nodes have an IOS console to scrape.
		if dn := ll.findNode(id); dn == nil || dn.Kind != lab.KindIOL {
			pn.Hint = "node has no IOS console"
			nodes[i] = pn
			continue
		}

		nr := ll.get(id)
		if nr == nil || nr.proc == nil {
			pn.Hint = "node is not running — start the lab to paint live protocol state"
			nodes[i] = pn
			continue
		}
		pn.Running = true

		wg.Add(1)
		sem <- struct{}{}
		go func(i, id int, pn protocol.PainterNode) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.collectNode(ctx, ll, id, proto, args.Dest, args.VLAN, &pn); err != nil {
				pn.Hint = "could not read live protocol state (still converging or console busy)"
			}
			nodes[i] = pn // distinct index per goroutine -> no lock needed
		}(i, id, pn)
	}
	wg.Wait()

	res.Nodes = nodes
	return res, nil
}

// collectNode runs the protocol's show command(s) on one node and fills pn.
func (s *Server) collectNode(ctx context.Context, ll *loadedLab, id int, proto, dest string, vlan int, pn *protocol.PainterNode) error {
	switch proto {
	case "stp":
		// Snapshot ONE VLAN's tree: `show spanning-tree vlan <N>` so a node
		// that runs STP on several VLANs never bleeds a different VLAN's
		// root/ports into this result (that was the root cause of the old
		// "two root crowns" bug — it ran the unqualified, all-VLAN command
		// and took whichever block happened first).
		out, err := s.runShow(ctx, ll, id, fmt.Sprintf("show spanning-tree vlan %d", vlan))
		if err != nil {
			return err
		}
		st := painter.ParseSTPVlanBlock(out, vlan)
		if st.Empty() {
			pn.Hint = "no spanning-tree data for this VLAN (not configured on this node, L3-only, or still converging)"
			return nil
		}
		pn.STP = mapSTP(st)

	case "ospf":
		nbrOut, err := s.runShow(ctx, ll, id, "show ip ospf neighbor")
		if err != nil {
			return err
		}
		o := protocol.PainterOSPF{Neighbors: []protocol.PainterOSPFNeighbor{}}
		for _, nb := range painter.ParseOSPFNeighbors(nbrOut) {
			o.Neighbors = append(o.Neighbors, protocol.PainterOSPFNeighbor{
				NeighborID:    nb.NeighborID,
				State:         nb.State,
				Role:          nb.Role,
				Address:       nb.Address,
				Interface:     nb.Interface,
				InterfaceNorm: nb.InterfaceNorm,
			})
		}
		if dest != "" {
			// Filter the routing table to the destination directly.
			rtOut, err := s.runShow(ctx, ll, id, "show ip route "+dest)
			if err == nil {
				if rt := painter.ParseOSPFRoute(rtOut, dest); rt.NextHop != "" {
					o.Route = &protocol.PainterRoute{
						Prefix:        rt.Prefix,
						NextHop:       rt.NextHop,
						Interface:     rt.Interface,
						InterfaceNorm: rt.InterfaceNorm,
						Cost:          rt.Cost,
					}
				}
			}
		}
		if len(o.Neighbors) == 0 && o.Route == nil {
			pn.Hint = "no OSPF neighbors (not configured or still converging)"
			return nil
		}
		pn.OSPF = &o

	case "eigrp":
		if dest == "" {
			pn.Hint = "select a destination to trace the EIGRP path"
			return nil
		}
		out, err := s.runShow(ctx, ll, id, "show ip eigrp topology "+dest)
		if err != nil {
			return err
		}
		e := painter.ParseEIGRPTopology(out)
		if e.Empty() {
			pn.Hint = "no EIGRP topology entry for this destination"
			return nil
		}
		paths := make([]protocol.PainterEIGRPPath, 0, len(e.Paths))
		for _, p := range e.Paths {
			paths = append(paths, protocol.PainterEIGRPPath{
				NextHop:           p.NextHop,
				Interface:         p.Interface,
				InterfaceNorm:     p.InterfaceNorm,
				FD:                p.FD,
				RD:                p.RD,
				Successor:         p.Successor,
				FeasibleSuccessor: p.FeasibleSuccessor,
			})
		}
		pn.EIGRP = &protocol.PainterEIGRP{Prefix: e.Prefix, FD: e.FD, Paths: paths, NextHop: e.NextHop}

	case "bgp":
		if dest == "" {
			pn.Hint = "select a destination prefix to show the BGP best path"
			return nil
		}
		out, err := s.runShow(ctx, ll, id, "show ip bgp "+dest)
		if err != nil {
			return err
		}
		b := painter.ParseBGP(out)
		if b.Empty() {
			pn.Hint = "prefix not in the BGP table on this node"
			return nil
		}
		paths := make([]protocol.PainterBGPPath, 0, len(b.Paths))
		for _, p := range b.Paths {
			paths = append(paths, protocol.PainterBGPPath{
				NextHop:   p.NextHop,
				ASPath:    p.ASPath,
				Origin:    p.Origin,
				Weight:    p.Weight,
				LocalPref: p.LocalPref,
				MED:       p.MED,
				Best:      p.Best,
			})
		}
		pn.BGP = &protocol.PainterBGP{Prefix: b.Prefix, Paths: paths, BestNextHop: b.BestNextHop, Reason: b.Reason}

	default:
		return protocol.Errorf(protocol.CodeBadRequest, "unknown painter proto %q", proto)
	}
	return nil
}

// mapSTP converts a painter.STPResult into the protocol result shape.
func mapSTP(st painter.STPResult) *protocol.PainterSTP {
	ports := make([]protocol.PainterSTPPort, 0, len(st.Ports))
	for _, p := range st.Ports {
		ports = append(ports, protocol.PainterSTPPort{
			Interface:     p.Interface,
			InterfaceNorm: p.InterfaceNorm,
			Role:          string(p.Role),
			State:         string(p.State),
			Cost:          p.Cost,
			Prio:          p.Prio,
			Blocked:       p.Blocked,
			Reason:        p.Reason,
		})
	}
	return &protocol.PainterSTP{
		VLAN:     st.VLAN,
		RootID:   st.RootID,
		BridgeID: st.BridgeID,
		IsRoot:   st.IsRoot,
		RootCost: st.RootCost,
		RootPort: st.RootPort,
		Ports:    ports,
	}
}

// painterSTPVlans runs the VLAN-enumeration step of the STP painter flow on
// ONE node: `show spanning-tree` (all-VLAN dump) to find which VLANs actually
// have an STP instance running, enriched with `show vlan brief` for names
// (best-effort — a parse failure there just leaves Name empty). Tolerant of
// L3 nodes / no STP configured: yields an empty Vlans list with a Hint, never
// an error.
func (s *Server) painterSTPVlans(ctx context.Context, ll *loadedLab, nodeID int) (protocol.PainterVlansResult, error) {
	res := protocol.PainterVlansResult{Node: nodeID, Vlans: []protocol.PainterVlan{}}

	if dn := ll.findNode(nodeID); dn == nil || dn.Kind != lab.KindIOL {
		res.Hint = "node has no IOS console"
		return res, nil
	}
	nr := ll.get(nodeID)
	if nr == nil || nr.proc == nil {
		res.Hint = "node is not running — start the lab to enumerate live STP VLANs"
		return res, nil
	}
	res.Running = true

	stpOut, err := s.runShow(ctx, ll, nodeID, "show spanning-tree")
	if err != nil {
		res.Hint = "could not read live STP state (console busy)"
		return res, nil
	}
	// Best-effort VLAN name enrichment; ignore errors, names stay empty.
	vlanBriefOut, _ := s.runShow(ctx, ll, nodeID, "show vlan brief")

	for _, v := range painter.ParseSTPVlans(stpOut, vlanBriefOut) {
		res.Vlans = append(res.Vlans, protocol.PainterVlan{ID: v.ID, Name: v.Name})
	}
	if len(res.Vlans) == 0 {
		res.Hint = "no spanning-tree VLANs on this node (L3-only or STP not configured)"
	}
	return res, nil
}
