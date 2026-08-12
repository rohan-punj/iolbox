package server

import (
	"encoding/json"
	"testing"

	"github.com/rohanpunj/iolbox/supervisor/internal/lab"
)

func TestMergePCStateWritesOnlyPCConfig(t *testing.T) {
	node := lab.Node{ID: 4, Kind: lab.KindPC, Name: "PC4", Config: map[string]json.RawMessage{
		"net":  json.RawMessage(`{"ip":"10.0.0.4","prefixLen":24}`),
		"pack": json.RawMessage(`"must-not-be-created"`),
	}}
	ll := newLoadedLab(&lab.Lab{Version: 1, ID: "pc", Name: "pc", Nodes: []lab.Node{node}}, t.TempDir())
	state := lab.PCState{DHCP: true, SavedCommands: []string{"show ip"}}
	if !mergePCState(ll, 4, state) {
		t.Fatal("merge rejected PC")
	}
	got := ll.findNode(4)
	if got.Name != "PC4" || string(got.Config["net"]) != `{"ip":"10.0.0.4","prefixLen":24}` || string(got.Config["pack"]) != `"must-not-be-created"` {
		t.Fatalf("unrelated fields changed: %#v", got)
	}
	var decoded lab.PCState
	if err := json.Unmarshal(got.Config["pc"], &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.DHCP || len(decoded.SavedCommands) != 1 {
		t.Fatalf("merged state = %#v", decoded)
	}
}

func TestValidatePCStateRejectsUnknownAndUnsafeContent(t *testing.T) {
	if _, err := validatePCState([]byte(`{"pc":{"dhcp":false,"savedCommands":[],"extra":1},"rev":1}`)); err == nil {
		t.Fatal("unknown state field accepted")
	}
	if _, err := validatePCState([]byte(`{"pc":{"dhcp":false,"savedCommands":["bad\u0001"]},"rev":1}`)); err == nil {
		t.Fatal("non-printable command accepted")
	}
}
