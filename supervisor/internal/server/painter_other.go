//go:build !linux

package server

import (
	"context"

	"github.com/rohanpunj/iolab/supervisor/internal/protocol"
)

// runShow is a stub off Linux — IOL nodes only spawn under the Linux pty console
// model (see spawn_linux.go), so there is no console to scrape here. It exists so
// the server package cross-compiles for GOOS=windows.
func (s *Server) runShow(ctx context.Context, ll *loadedLab, nodeID int, cmd string) (string, error) {
	return "", protocol.Errorf(protocol.CodeNotLoaded, "console scrape is only supported on the Linux runtime")
}

// painterCollect is a stub off Linux: no nodes run here, so every targeted node
// reports not-running with a hint. Keeps the Windows cross-compile green.
func (s *Server) painterCollect(ctx context.Context, ll *loadedLab, args protocol.PainterArgs) (protocol.PainterResult, error) {
	res := protocol.PainterResult{Proto: args.Proto, Dest: args.Dest, Nodes: []protocol.PainterNode{}}
	for _, n := range ll.doc.Nodes {
		res.Nodes = append(res.Nodes, protocol.PainterNode{
			Node: n.ID,
			Hint: "protocol painter runs on the Linux runtime only",
		})
	}
	return res, nil
}
