package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDarwinPortContractEnumeratesExactRanges(t *testing.T) {
	ports, err := newDarwinPortContract(defaultDarwinGUIPort)
	if err != nil {
		t.Fatal(err)
	}
	got := ports.requiredPorts()
	if len(got) != 81 {
		t.Fatalf("required port count = %d, want 81", len(got))
	}
	if got[0] != defaultDarwinGUIPort || got[1] != darwinConsoleStart || got[50] != darwinConsoleEnd || got[51] != darwinCaptureStart || got[80] != darwinCaptureEnd {
		t.Fatalf("required port endpoints = %v", got)
	}
	seen := make(map[int]bool, len(got))
	for _, port := range got {
		if seen[port] {
			t.Fatalf("duplicate required port %d", port)
		}
		seen[port] = true
	}
}

func TestDarwinPortContractRejectsInvalidGUIOverrides(t *testing.T) {
	for _, port := range []int{darwinControlPort, darwinConsoleStart, darwinConsoleEnd, darwinCaptureStart, darwinCaptureEnd, 65536} {
		if _, err := newDarwinPortContract(port); err == nil {
			t.Errorf("GUI port %d was accepted", port)
		}
	}
	for _, port := range []int{1, 3999, 4001, 65535} {
		if _, err := newDarwinPortContract(port); err != nil {
			t.Errorf("GUI port %d rejected: %v", port, err)
		}
	}
}

func TestDarwinPortContractRendersAllRulesInOrder(t *testing.T) {
	ports, err := newDarwinPortContract(4101)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := parseDarwinPortForwardRules(ports.yamlPortForwards())
	if err != nil {
		t.Fatal(err)
	}
	want := expectedDarwinPortForwardRules(ports)
	if len(rules) != len(want) {
		t.Fatalf("port-forward rule count = %d, want %d: %#v", len(rules), len(want), rules)
	}
	for i := range want {
		if !darwinPortForwardRulesEqual(rules[i], want[i]) {
			t.Fatalf("port-forward rule %d = %#v, want %#v", i, rules[i], want[i])
		}
	}
	setExpr := ports.limaStartSetArg()
	if !strings.HasPrefix(setExpr, "--set=.portForwards=") || !strings.HasSuffix(setExpr, "\"ignore\": true}]") {
		t.Fatalf("unexpected Lima --set argument: %s", setExpr)
	}
}

// TestDarwinPortForwardRulesAcceptBlockSequenceForm reproduces a lima.yaml
// observed on real hardware after a forced-kill/restart recovery cycle:
// Lima's own YAML marshaler had resaved the config with guestPortRange/
// hostPortRange as a block sequence (key on its own line, items indented
// below) instead of the flow form this launcher writes when first creating
// a VM (guestPortRange: [9000, 9049]). Before this fix, the parser treated
// the empty-value "guestPortRange:" line as a hard error ("invalid
// portForwards field"), so a perfectly healthy, already-running VM could
// never pass its own port-contract check again — every restart failed.
func TestDarwinPortForwardRulesAcceptBlockSequenceForm(t *testing.T) {
	yaml := `
portForwards:
- guestPort: 4001
  hostPort: 4001
  hostIP: 127.0.0.1
  proto: tcp
- guestPortRange:
  - 9000
  - 9049
  hostPortRange:
  - 9000
  - 9049
  hostIP: 127.0.0.1
  proto: tcp
- guestPortRange:
  - 5500
  - 5529
  hostPortRange:
  - 5500
  - 5529
  hostIP: 127.0.0.1
  proto: tcp
- guestIP: 127.0.0.1
  guestPortRange:
  - 1
  - 65535
  proto: any
  ignore: true
`
	rules, err := parseDarwinPortForwardRules(yaml)
	if err != nil {
		t.Fatalf("parseDarwinPortForwardRules(block form) error: %v", err)
	}
	if len(rules) != 4 {
		t.Fatalf("rule count = %d, want 4: %#v", len(rules), rules)
	}
	if rules[1]["guestPortRange"] != "[9000,9049]" || rules[1]["hostPortRange"] != "[9000,9049]" {
		t.Fatalf("console range rule = %#v", rules[1])
	}
	if rules[2]["guestPortRange"] != "[5500,5529]" || rules[2]["hostPortRange"] != "[5500,5529]" {
		t.Fatalf("capture range rule = %#v", rules[2])
	}

	ports, err := newDarwinPortContract(4001)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := darwinPortContractMatchesYAML([]byte(yaml), ports)
	if err != nil {
		t.Fatalf("darwinPortContractMatchesYAML(block form) error: %v", err)
	}
	if !ok {
		t.Fatal("darwinPortContractMatchesYAML(block form) = false, want true — a Lima-resaved config must still satisfy the same contract it was created with")
	}
}

func TestDarwinPortConflictsReportsEveryPort(t *testing.T) {
	ports, err := newDarwinPortContract(defaultDarwinGUIPort)
	if err != nil {
		t.Fatal(err)
	}
	conflicts := darwinPortConflicts(ports, func(port int) error {
		if port == defaultDarwinGUIPort || port == darwinConsoleEnd || port == darwinCaptureStart {
			return errors.New("busy")
		}
		return nil
	})
	if len(conflicts) != 3 || conflicts[0] != defaultDarwinGUIPort || conflicts[1] != darwinConsoleEnd || conflicts[2] != darwinCaptureStart {
		t.Fatalf("conflicts = %v", conflicts)
	}
}
