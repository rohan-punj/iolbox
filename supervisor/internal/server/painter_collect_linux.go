//go:build linux

package server

import (
	"context"
	"strings"

	"github.com/rohanpunj/iolab/supervisor/internal/lab"
	"github.com/rohanpunj/iolab/supervisor/internal/painter"
	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// painterCollect scrapes the requested protocol's `show` output from every
// targeted RUNNING IOL node, parses it (internal/painter), and maps it into the
// protocol result shape the frontend consumes. Non-running / no-data nodes get
// an explicit empty entry with a Hint — never fabricated data.
func (s *Server) painterCollect(ctx context.Context, ll *loadedLab, args protocol.PainterArgs) (protocol.PainterResult, error) {
	proto := strings.ToLower(strings.TrimSpace(args.Proto))
	res := protocol.PainterResult{Proto: proto, Dest: args.Dest, Nodes: []protocol.PainterNode{}}

	ids := args.Nodes
	if ids == nil {
		for _, n := range ll.doc.Nodes {
			if n.Kind == lab.KindIOL {
				ids = append(ids, n.ID)
			}
		}
	}

	for _, id := range ids {
		pn := protocol.PainterNode{Node: id}

		// Only IOL nodes have an IOS console to scrape.
		if dn := ll.findNode(id); dn == nil || dn.Kind != lab.KindIOL {
			pn.Hint = "node has no IOS console"
			res.Nodes = append(res.Nodes, pn)
			continue
		}

		nr := ll.get(id)
		if nr == nil || nr.proc == nil {
			pn.Hint = "node is not running — start the lab to paint live protocol state"
			res.Nodes = append(res.Nodes, pn)
			continue
		}
		pn.Running = true

		if err := s.collectNode(ctx, ll, id, proto, args.Dest, &pn); err != nil {
			pn.Hint = "could not read live protocol state (still converging or console busy)"
		}
		res.Nodes = append(res.Nodes, pn)
	}
	return res, nil
}

// collectNode runs the protocol's show command(s) on one node and fills pn.
func (s *Server) collectNode(ctx context.Context, ll *loadedLab, id int, proto, dest string, pn *protocol.PainterNode) error {
	switch proto {
	case "stp":
		out, err := s.runShow(ctx, ll, id, "show spanning-tree")
		if err != nil {
			return err
		}
		st := painter.ParseSTP(out)
		if st.Empty() {
			pn.Hint = "no spanning-tree data (not configured or still converging)"
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
		RootID:   st.RootID,
		BridgeID: st.BridgeID,
		IsRoot:   st.IsRoot,
		RootCost: st.RootCost,
		RootPort: st.RootPort,
		Ports:    ports,
	}
}
