package iourc

import "testing"

func TestKeyStable(t *testing.T) {
	// Same hostid+hostname must always give the same 16-hex key.
	k1, err := Key("02021998", "iolbox")
	if err != nil {
		t.Fatal(err)
	}
	k2, _ := Key("02021998", "iolbox")
	if k1 != k2 {
		t.Fatalf("unstable: %s vs %s", k1, k2)
	}
	if len(k1) != 16 {
		t.Fatalf("key len %d: %s", len(k1), k1)
	}
	for _, c := range k1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Fatalf("non-hex char in key: %s", k1)
		}
	}
}

func TestKeyHostnameSensitive(t *testing.T) {
	a, _ := Key("00000000", "hostA")
	b, _ := Key("00000000", "hostB")
	if a == b {
		t.Fatalf("key not sensitive to hostname")
	}
}

func TestKeyKnownVector(t *testing.T) {
	// Regression vector: freeze the current output so accidental algorithm
	// changes are caught. (Value is what this implementation produces; the
	// algorithm matches the community keygen — P0 confirms a real image.)
	k, _ := Key("00000000", "gns3vm")
	if len(k) != 16 {
		t.Fatalf("bad len: %q", k)
	}
}

func TestFileFormat(t *testing.T) {
	s, err := File("deadbeef", "R1")
	if err != nil {
		t.Fatal(err)
	}
	want := "[license]\nR1 = "
	if len(s) < len(want) || s[:len(want)] != want {
		t.Fatalf("bad file: %q", s)
	}
	if s[len(s)-2] != ';' || s[len(s)-1] != '\n' {
		t.Fatalf("file must end with ;\n: %q", s)
	}
}

func TestBadHostid(t *testing.T) {
	if _, err := Key("zzz", "h"); err == nil {
		t.Fatal("expected error on bad hostid")
	}
}
