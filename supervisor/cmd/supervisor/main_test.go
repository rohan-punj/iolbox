package main

import "testing"

func TestDisableI386FromEnv(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{value: "", want: false},
		{value: "0", want: false},
		{value: "true", want: false},
		{value: "1", want: true},
		{value: "1 ", want: false},
	} {
		if got := disableI386FromEnv(tc.value); got != tc.want {
			t.Fatalf("disableI386FromEnv(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}
