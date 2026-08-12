package main

import (
	"reflect"
	"testing"
)

func TestParseCapsListEmpty(t *testing.T) {
	got, err := parseCapsList("")
	if err != nil {
		t.Fatalf("parseCapsList(\"\") error: %v", err)
	}
	if got != nil {
		t.Fatalf("parseCapsList(\"\") = %#v, want nil", got)
	}
}

func TestParseCapsListSingle(t *testing.T) {
	got, err := parseCapsList("cap_net_raw")
	if err != nil {
		t.Fatalf("parseCapsList error: %v", err)
	}
	want := []string{"cap_net_raw"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCapsList = %#v, want %#v", got, want)
	}
}

func TestParseCapsListMulti(t *testing.T) {
	got, err := parseCapsList("cap_net_raw,cap_net_admin")
	if err != nil {
		t.Fatalf("parseCapsList error: %v", err)
	}
	want := []string{"cap_net_raw", "cap_net_admin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCapsList = %#v, want %#v", got, want)
	}
}

func TestParseCapsListUnknown(t *testing.T) {
	if _, err := parseCapsList("cap_sys_admin"); err == nil {
		t.Fatal("parseCapsList(\"cap_sys_admin\") should have been rejected — not in knownCapNumbers")
	}
}

func TestParseLaunchArgsMultiCap(t *testing.T) {
	opts, err := parseLaunchArgs([]string{
		"--user", "ioltool", "--caps", "cap_net_raw,cap_net_admin", "--", "/opt/pack/pc-gui", "--serve",
	})
	if err != nil {
		t.Fatalf("parseLaunchArgs error: %v", err)
	}
	want := []string{"cap_net_raw", "cap_net_admin"}
	if !reflect.DeepEqual(opts.caps, want) {
		t.Fatalf("opts.caps = %#v, want %#v", opts.caps, want)
	}
	if opts.target != "/opt/pack/pc-gui" {
		t.Fatalf("opts.target = %q", opts.target)
	}
}

func TestParseLaunchArgsEmptyCaps(t *testing.T) {
	opts, err := parseLaunchArgs([]string{
		"--user", "ioltool", "--caps", "", "--", "/opt/pack/webserver-gui",
	})
	if err != nil {
		t.Fatalf("parseLaunchArgs error: %v", err)
	}
	if opts.caps != nil {
		t.Fatalf("opts.caps = %#v, want nil (zero capabilities)", opts.caps)
	}
}

func TestParseLaunchArgsRequiresCapsFlag(t *testing.T) {
	if _, err := parseLaunchArgs([]string{"--user", "ioltool", "--", "/opt/pack/tool"}); err == nil {
		t.Fatal("parseLaunchArgs should require --caps even when the value can be empty")
	}
}

func TestParseLaunchArgsRejectsUnknownCap(t *testing.T) {
	if _, err := parseLaunchArgs([]string{
		"--user", "ioltool", "--caps", "cap_sys_admin", "--", "/opt/pack/tool",
	}); err == nil {
		t.Fatal("parseLaunchArgs should reject a capability outside knownCapNumbers")
	}
}
