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
