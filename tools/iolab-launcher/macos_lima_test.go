package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseMachineListing(t *testing.T) {
	machines, err := parseMachineListing("m1|Running\r\nm2|Stopped\n")
	if err != nil || len(machines) != 2 || machines[0].State != "Running" || machines[1].Name != "m2" {
		t.Fatalf("machines/error = %#v/%v", machines, err)
	}
	if _, err := parseMachineListing("malformed"); err == nil {
		t.Fatal("malformed machine listing was accepted")
	}
}

func TestLimaStartWithPortContractKeepsExpressionInOneArg(t *testing.T) {
	runner := &sequenceRunner{}
	client := &limaClient{info: limaInfo{Path: "limactl"}, runner: runner}
	ports, err := newDarwinPortContract(defaultDarwinGUIPort)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.startWithPortContract(t.Context(), "m3", ports); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || len(runner.calls[0]) != 5 {
		t.Fatalf("Lima start argv = %v", runner.calls)
	}
	if runner.calls[0][1] != "start" || runner.calls[0][2] != "m3" || runner.calls[0][4] != "--tty=false" {
		t.Fatalf("Lima start ordering = %v", runner.calls[0])
	}
	if !strings.HasPrefix(runner.calls[0][3], "--set=.portForwards=") || !strings.Contains(runner.calls[0][3], "\"hostIP\": \"127.0.0.1\"") {
		t.Fatalf("Lima port expression = %q", runner.calls[0][3])
	}
}

func TestSelectPayloadNewestThenLexicalTieBreak(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "iolbox-server-a.tar.gz")
	second := filepath.Join(dir, "iolbox-server-b.tar.gz")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	same := time.Unix(1234, 0)
	if err := os.Chtimes(first, same, same); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(second, same, same); err != nil {
		t.Fatal(err)
	}
	got, err := selectPayload("", dir)
	if err != nil || got != first {
		t.Fatalf("payload/error = %q/%v, want %q", got, err, first)
	}
	newer := filepath.Join(dir, "iolbox-server-new.tar.gz")
	if err := os.WriteFile(newer, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, same.Add(time.Hour), same.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err = selectPayload("", dir)
	if err != nil || got != newer {
		t.Fatalf("newest payload/error = %q/%v, want %q", got, err, newer)
	}
}
