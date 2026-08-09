package main

import (
	"reflect"
	"testing"
)

func TestStripIfaceFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "double dash two token", args: []string{"--iface", "eth2"}},
		{name: "single dash long two token", args: []string{"-iface", "eth2"}},
		{name: "double dash glued", args: []string{"--iface=eth2"}},
		{name: "single dash long glued", args: []string{"-iface=eth2"}},
		{name: "short two token", args: []string{"-i", "eth2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--count", "3"}, tt.args...)
			args = append(args, "--selftest")
			want := []string{"--count", "3", "--selftest"}
			if got := stripIfaceFlag(args); !reflect.DeepEqual(got, want) {
				t.Fatalf("stripIfaceFlag(%v) = %v, want %v", args, got, want)
			}
		})
	}

	benign := []string{"--count", "3", "--interval", "0.25", "--selftest", "--target_ip", "192.0.2.1"}
	if got := stripIfaceFlag(benign); !reflect.DeepEqual(got, benign) {
		t.Fatalf("stripIfaceFlag changed benign args: got %v, want %v", got, benign)
	}
}
