package tool

import (
	"reflect"
	"strings"
	"testing"
)

func TestCageSelectRoot(t *testing.T) {
	cases := []struct {
		name       string
		discovered string
		delegated  string
		leaf       string
	}{
		{
			name:       "fresh delegated service cgroup",
			discovered: "/system.slice/iolbox-supervisor.service",
			delegated:  "/system.slice/iolbox-supervisor.service",
			leaf:       "/system.slice/iolbox-supervisor.service/supervisor",
		},
		{
			name:       "repeat call after supervisor migration",
			discovered: "/system.slice/iolbox-supervisor.service/supervisor",
			delegated:  "/system.slice/iolbox-supervisor.service",
			leaf:       "/system.slice/iolbox-supervisor.service/supervisor",
		},
		{
			name:       "contains supervisor but is not the leaf",
			discovered: "/system.slice/supervisor-2",
			delegated:  "/system.slice/supervisor-2",
			leaf:       "/system.slice/supervisor-2/supervisor",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delegated, leaf := cageSelectRoot(tc.discovered)
			if delegated != tc.delegated || leaf != tc.leaf {
				t.Fatalf("cageSelectRoot(%q) = %q, %q; want %q, %q", tc.discovered, delegated, leaf, tc.delegated, tc.leaf)
			}
		})
	}
}

func TestCageParseProcCgroup(t *testing.T) {
	contents := "11:memory:/legacy\n0::/system.slice/iolbox-supervisor.service\n"
	got, err := cageParseProcCgroup(contents)
	if err != nil {
		t.Fatalf("cageParseProcCgroup returned error: %v", err)
	}
	if got != "/system.slice/iolbox-supervisor.service" {
		t.Fatalf("cageParseProcCgroup = %q", got)
	}
	if _, err := cageParseProcCgroup("0:memory:/not-unified\n"); err == nil {
		t.Fatal("cageParseProcCgroup accepted a missing unified hierarchy")
	}
}

func TestCageParsePopulated(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    bool
		bad     bool
	}{
		{name: "populated", fixture: "populated 1\nfrozen 0\n", want: true},
		{name: "empty", fixture: "populated 0\nfrozen 0\n", want: false},
		{name: "malformed value", fixture: "populated yes\n", bad: true},
		{name: "malformed line", fixture: "populated 0\nbroken\n", bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cageParsePopulated(tc.fixture)
			if tc.bad {
				if err == nil {
					t.Fatalf("cageParsePopulated(%q) accepted malformed fixture", tc.fixture)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("cageParsePopulated(%q) = %v, %v; want %v, nil", tc.fixture, got, err, tc.want)
			}
		})
	}
}

func TestCageLimitFormattingAndOrder(t *testing.T) {
	limits := Limits{MemoryMax: 123456, PidsMax: 17, CPUMax: "12345 100000", SwapMax: 0}
	writes := cageLimitWrites("/delegated/tool-7", limits)
	wantNames := []string{"memory.max", "pids.max", "cpu.max", "memory.swap.max"}
	wantValues := []string{"123456", "17", "12345 100000", "0"}
	for index, write := range writes {
		if write.name != wantNames[index] || write.value != wantValues[index] {
			t.Fatalf("limit write %d = %+v; want %q=%q", index, write, wantNames[index], wantValues[index])
		}
	}
	if got := cageCreateOrder(); !reflect.DeepEqual(got, []string{"memory.max", "pids.max", "cpu.max", "memory.swap.max", "open"}) {
		t.Fatalf("cageCreateOrder = %#v", got)
	}
	if strings.Index(strings.Join(cageCreateOrder(), ","), "open") < strings.Index(strings.Join(cageCreateOrder(), ","), "memory.swap.max") {
		t.Fatal("fd open precedes the final resource-limit write")
	}
}
