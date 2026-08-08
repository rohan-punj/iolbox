package lab

import (
	"encoding/json"
	"testing"
)

func goodLab() *Lab {
	eth := 1
	return &Lab{
		Version: 1, ID: "lab-1", Name: "Test",
		Nodes: []Node{
			{ID: 0, Kind: KindIOL, Name: "R1", Image: &ImageRef{ID: "abc123"}, Ethernet: &eth},
			{ID: 1, Kind: KindIOL, Name: "R2", Image: &ImageRef{ID: "abc123"}},
			{ID: 2, Kind: KindVPCS, Name: "PC1"},
		},
		Links: []Link{
			{ID: 0, Type: LinkP2P, Endpoints: []Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "e0/0"}}},
			{ID: 1, Type: LinkSegment, Endpoints: []Endpoint{{Node: 1, Interface: "e0/1"}, {Node: 2, Interface: "eth0"}, {Node: 0, Interface: "e0/2"}}},
		},
	}
}

func TestValidateGood(t *testing.T) {
	if err := goodLab().Validate(); err != nil {
		t.Fatalf("good lab failed: %v", err)
	}
}

func TestValidateBad(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Lab)
	}{
		{"wrong version", func(l *Lab) { l.Version = 2 }},
		{"empty id", func(l *Lab) { l.ID = "" }},
		{"empty name", func(l *Lab) { l.Name = "" }},
		{"dup node id", func(l *Lab) { l.Nodes[1].ID = 0 }},
		{"iol no image", func(l *Lab) { l.Nodes[0].Image = nil }},
		{"iol empty image id", func(l *Lab) { l.Nodes[0].Image = &ImageRef{} }},
		{"bad kind", func(l *Lab) { l.Nodes[0].Kind = "router" }},
		{"p2p 3 endpoints", func(l *Lab) {
			l.Links[0].Endpoints = append(l.Links[0].Endpoints, Endpoint{Node: 2, Interface: "eth0"})
		}},
		{"link 1 endpoint", func(l *Lab) { l.Links[0].Endpoints = l.Links[0].Endpoints[:1] }},
		{"dup link id", func(l *Lab) { l.Links[1].ID = 0 }},
		{"unknown node ref", func(l *Lab) { l.Links[0].Endpoints[0].Node = 99 }},
		{"bad iol iface", func(l *Lab) { l.Links[0].Endpoints[0].Interface = "e0/99" }},
		{"eth out of range", func(l *Lab) { e := 99; l.Nodes[0].Ethernet = &e }},
		{"ram too low", func(l *Lab) { l.Nodes[0].RAM = 8 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := goodLab()
			c.mutate(l)
			if err := l.Validate(); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
}

// TestValidateNatNodes confirms nat is an accepted kind with an eth0 interface,
// and that its constraints (interface must be eth0; at most one link endpoint)
// are enforced.
func TestValidateNatNodes(t *testing.T) {
	good := &Lab{
		Version: 1, ID: "lab-n", Name: "n",
		Nodes: []Node{
			{ID: 0, Kind: KindIOL, Name: "R1", Image: &ImageRef{ID: "abc"}},
			{ID: 1, Kind: KindNAT, Name: "Internet"},
		},
		Links: []Link{
			{ID: 0, Type: LinkP2P, Endpoints: []Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"}}},
		},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid nat lab rejected: %v", err)
	}

	bad := []struct {
		name   string
		mutate func(*Lab)
	}{
		{"nat non-eth0 iface", func(l *Lab) { l.Links[0].Endpoints[1].Interface = "e0/0" }},
		{"nat two links", func(l *Lab) {
			l.Links = append(l.Links, Link{ID: 2, Type: LinkP2P,
				Endpoints: []Endpoint{{Node: 0, Interface: "e0/2"}, {Node: 1, Interface: "eth0"}}})
		}},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			l := &Lab{
				Version: 1, ID: "lab-n", Name: "n",
				Nodes: []Node{
					{ID: 0, Kind: KindIOL, Name: "R1", Image: &ImageRef{ID: "abc"}},
					{ID: 1, Kind: KindNAT, Name: "Internet"},
				},
				Links: []Link{
					{ID: 0, Type: LinkP2P, Endpoints: []Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth0"}}},
				},
			}
			c.mutate(l)
			if err := l.Validate(); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
}

// TestValidateToolNodes confirms a tool node's structural pack and endpoint
// constraints remain independent of the installed-pack registry.
func TestValidateToolNodes(t *testing.T) {
	good := &Lab{
		Version: 1, ID: "lab-tool", Name: "tool",
		Nodes: []Node{
			{ID: 0, Kind: KindIOL, Name: "R1", Image: &ImageRef{ID: "abc"}},
			{ID: 1, Kind: KindTool, Name: "Secbench", Config: map[string]json.RawMessage{
				"pack": json.RawMessage(`"stub"`),
			}},
		},
		Links: []Link{{
			ID: 0, Type: LinkP2P,
			Endpoints: []Endpoint{{Node: 0, Interface: "e0/0"}, {Node: 1, Interface: "eth1"}},
		}},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid tool lab rejected: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Lab)
	}{
		{"missing pack", func(l *Lab) { l.Nodes[1].Config = nil }},
		{"empty pack", func(l *Lab) {
			l.Nodes[1].Config = map[string]json.RawMessage{"pack": json.RawMessage(`""`)}
		}},
		{"pack is not a string", func(l *Lab) {
			l.Nodes[1].Config = map[string]json.RawMessage{"pack": json.RawMessage(`123`)}
		}},
		{"non-eth1 interface", func(l *Lab) {
			l.Links[0].Endpoints[1].Interface = "eth0"
		}},
		{"two link endpoints", func(l *Lab) {
			l.Links = append(l.Links, Link{ID: 1, Type: LinkP2P, Endpoints: []Endpoint{
				{Node: 0, Interface: "e0/1"}, {Node: 1, Interface: "eth1"},
			}})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := *good
			l.Nodes = append([]Node(nil), good.Nodes...)
			l.Links = append([]Link(nil), good.Links...)
			for i := range l.Links {
				l.Links[i].Endpoints = append([]Endpoint(nil), good.Links[i].Endpoints...)
			}
			c.mutate(&l)
			if err := l.Validate(); err == nil {
				t.Fatalf("expected error for %q", c.name)
			}
		})
	}
}

func TestUnmarshalRoundTrip(t *testing.T) {
	raw := []byte(`{"version":1,"id":"x","name":"n","nodes":[{"id":0,"kind":"vpcs","name":"PC","x":1,"y":2}],"links":[]}`)
	l, err := Unmarshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if l.Nodes[0].Kind != KindVPCS {
		t.Fatalf("kind=%q", l.Nodes[0].Kind)
	}
}
