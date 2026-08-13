package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
	"github.com/rohanpunj/iolbox/supervisor/internal/protocol"
)

const (
	pcStateMaxBytes       = 64 * 1024
	pcStateMaxCommands    = 64
	pcStateMaxCommandByte = 200
)

type pcStateEnvelope struct {
	PC  lab.PCState `json:"pc"`
	Rev int64       `json:"rev"`
}

func clonePCState(in lab.PCState) lab.PCState {
	out := in
	out.SavedCommands = append([]string(nil), in.SavedCommands...)
	if out.SavedCommands == nil {
		out.SavedCommands = []string{}
	}
	return out
}

func validatePCState(body []byte) (lab.PCState, error) {
	if len(body) > pcStateMaxBytes {
		return lab.PCState{}, fmt.Errorf("state document exceeds %d bytes", pcStateMaxBytes)
	}
	var envelope pcStateEnvelope
	dec := json.NewDecoder(bytesReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&envelope); err != nil {
		return lab.PCState{}, fmt.Errorf("decode state: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return lab.PCState{}, fmt.Errorf("state document has trailing data")
	}
	state := clonePCState(envelope.PC)
	if len(state.SavedCommands) > pcStateMaxCommands {
		return lab.PCState{}, fmt.Errorf("savedCommands exceeds %d entries", pcStateMaxCommands)
	}
	for _, command := range state.SavedCommands {
		if len([]byte(command)) > pcStateMaxCommandByte {
			return lab.PCState{}, fmt.Errorf("saved command exceeds %d bytes", pcStateMaxCommandByte)
		}
		for _, b := range []byte(command) {
			if b < 0x20 || b > 0x7e {
				return lab.PCState{}, fmt.Errorf("saved command contains non-printable ASCII")
			}
		}
	}
	return state, nil
}

// bytesReader is kept local to make the strict decoder easy to exercise in
// tests without exposing another package-level transport abstraction.
func bytesReader(b []byte) io.Reader { return &sliceReader{b: b} }

type sliceReader struct {
	b []byte
	i int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

func pullPCStateSocket(socket string) (lab.PCState, error) {
	if socket == "" {
		return lab.PCState{}, fmt.Errorf("PC GUI socket is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	transport := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", socket, 2*time.Second)
		},
		DisableKeepAlives: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://iolbox/_iolbox/state", nil)
	if err != nil {
		return lab.PCState{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return lab.PCState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return lab.PCState{}, fmt.Errorf("PC state endpoint returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, pcStateMaxBytes+1))
	if err != nil {
		return lab.PCState{}, err
	}
	return validatePCState(body)
}

func (s *Server) previousPCState(ll *loadedLab, id int) lab.PCState {
	state := lab.PCState{SavedCommands: []string{}}
	ll.mu.Lock()
	defer ll.mu.Unlock()
	for i := range ll.doc.Nodes {
		if ll.doc.Nodes[i].ID != id || ll.doc.Nodes[i].Kind != lab.KindPC {
			continue
		}
		if raw := ll.doc.Nodes[i].Config["pc"]; len(raw) != 0 {
			var decoded lab.PCState
			if json.Unmarshal(raw, &decoded) == nil {
				state = clonePCState(decoded)
			}
		}
		break
	}
	return state
}

func (s *Server) syncPCNode(ll *loadedLab, id int) protocol.PCStateData {
	state := s.previousPCState(ll, id)
	ll.mu.Lock()
	nr := ll.nodes[id]
	// findNode takes ll.mu itself; calling it here was finding #12, a
	// non-reentrant self-deadlock that hung every stop of a running PC/tool
	// node (stopNode -> syncPCNode) with ll.mu — and the caller's s.labMu —
	// held forever. Use the locked variant so nr and the node document are
	// still read atomically under the one critical section.
	n := ll.findNodeLocked(id)
	ll.mu.Unlock()
	if n == nil || n.Kind != lab.KindPC || nr == nil || nr.tool == nil {
		data := protocol.PCStateData{Node: id, State: &state, Stale: true}
		s.emit(protocol.EventNodePCState, data)
		return data
	}
	pulled, err := pullPCStateSocket(nr.tool.SocketPath())
	if err != nil {
		log.Printf("supervisor: warning: pc %d state pull: %v", id, err)
		data := protocol.PCStateData{Node: id, State: &state, Stale: true}
		s.emit(protocol.EventNodePCState, data)
		return data
	}
	state = clonePCState(pulled)
	mergePCState(ll, id, state)
	data := protocol.PCStateData{Node: id, State: &state, Stale: false}
	s.emit(protocol.EventNodePCState, data)
	return data
}

func mergePCState(ll *loadedLab, id int, state lab.PCState) bool {
	raw, _ := json.Marshal(clonePCState(state))
	ll.mu.Lock()
	defer ll.mu.Unlock()
	for i := range ll.doc.Nodes {
		if ll.doc.Nodes[i].ID == id && ll.doc.Nodes[i].Kind == lab.KindPC {
			if ll.doc.Nodes[i].Config == nil {
				ll.doc.Nodes[i].Config = make(map[string]json.RawMessage)
			}
			ll.doc.Nodes[i].Config["pc"] = raw
			return true
		}
	}
	return false
}

func (s *Server) handlePCSyncState(raw json.RawMessage) (any, error) {
	var args protocol.PCStateSyncArgs
	if err := decode(raw, &args); err != nil {
		return nil, err
	}
	ll, err := s.currentLab(args.LabID)
	if err != nil {
		return nil, err
	}
	ids := []int{}
	if args.Node != nil {
		ids = append(ids, *args.Node)
	} else {
		ll.mu.Lock()
		for id, nr := range ll.nodes {
			if nr.tool == nil {
				continue
			}
			for _, n := range ll.doc.Nodes {
				if n.ID == id && n.Kind == lab.KindPC {
					ids = append(ids, id)
					break
				}
			}
		}
		ll.mu.Unlock()
	}
	result := protocol.PCStateSyncResult{States: make([]protocol.PCStateData, 0, len(ids))}
	for _, id := range ids {
		result.States = append(result.States, s.syncPCNode(ll, id))
	}
	return result, nil
}
