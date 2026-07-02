package telnet

import (
	"bytes"
	"testing"
)

func TestFeedPlainDataPassesThrough(t *testing.T) {
	n := NewNegotiator()
	out := n.Feed([]byte("hello world\r\n"))
	if string(out) != "hello world\r\n" {
		t.Fatalf("got %q", out)
	}
	if r := n.Reply(); r != nil {
		t.Fatalf("expected no reply, got %v", r)
	}
}

func TestEscapedIACLiteralPassesThrough(t *testing.T) {
	n := NewNegotiator()
	// IAC IAC in the stream represents a literal 0xFF data byte.
	out := n.Feed([]byte{'a', IAC, IAC, 'b'})
	want := []byte{'a', 0xFF, 'b'}
	if !bytes.Equal(out, want) {
		t.Fatalf("got %v want %v", out, want)
	}
}

func TestWillEchoAccepted(t *testing.T) {
	n := NewNegotiator()
	// Node sends IAC WILL ECHO.
	out := n.Feed([]byte{IAC, WILL, OptEcho})
	if len(out) != 0 {
		t.Fatalf("expected no application data, got %v", out)
	}
	reply := n.Reply()
	want := []byte{IAC, DO, OptEcho}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestWillSGAAccepted(t *testing.T) {
	n := NewNegotiator()
	out := n.Feed([]byte{IAC, WILL, OptSGA})
	if len(out) != 0 {
		t.Fatalf("expected no application data, got %v", out)
	}
	reply := n.Reply()
	want := []byte{IAC, DO, OptSGA}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestWillUnsupportedOptionRefused(t *testing.T) {
	n := NewNegotiator()
	const unsupported = 99
	n.Feed([]byte{IAC, WILL, unsupported})
	reply := n.Reply()
	want := []byte{IAC, DONT, unsupported}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestDoSGAAccepted(t *testing.T) {
	n := NewNegotiator()
	// Node asks US to enable SGA.
	n.Feed([]byte{IAC, DO, OptSGA})
	reply := n.Reply()
	want := []byte{IAC, WILL, OptSGA}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestDoUnsupportedOptionRefused(t *testing.T) {
	n := NewNegotiator()
	n.Feed([]byte{IAC, DO, OptTType})
	reply := n.Reply()
	want := []byte{IAC, WONT, OptTType}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestDontAlwaysAcknowledged(t *testing.T) {
	n := NewNegotiator()
	n.Feed([]byte{IAC, DONT, OptEcho})
	reply := n.Reply()
	want := []byte{IAC, WONT, OptEcho}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestWontAlwaysAcknowledged(t *testing.T) {
	n := NewNegotiator()
	n.Feed([]byte{IAC, WONT, OptEcho})
	reply := n.Reply()
	want := []byte{IAC, DONT, OptEcho}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestMixedDataAndNegotiation(t *testing.T) {
	n := NewNegotiator()
	// "AB" + IAC WILL ECHO + "CD" + IAC DO SGA + "EF" all in one Feed call,
	// simulating a single TCP read that straddles negotiation and data.
	input := []byte{'A', 'B', IAC, WILL, OptEcho, 'C', 'D', IAC, DO, OptSGA, 'E', 'F'}
	out := n.Feed(input)
	if string(out) != "ABCDEF" {
		t.Fatalf("got %q", out)
	}
	reply := n.Reply()
	want := []byte{IAC, DO, OptEcho, IAC, WILL, OptSGA}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestNegotiationSplitAcrossFeedCalls(t *testing.T) {
	n := NewNegotiator()
	// IAC arrives in one read, WILL+opt arrive in the next (TCP makes no
	// promise about where reads split).
	out1 := n.Feed([]byte{'x', IAC})
	if string(out1) != "x" {
		t.Fatalf("out1 = %q", out1)
	}
	out2 := n.Feed([]byte{WILL, OptEcho, 'y'})
	if string(out2) != "y" {
		t.Fatalf("out2 = %q", out2)
	}
	reply := n.Reply()
	want := []byte{IAC, DO, OptEcho}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %v, want %v", reply, want)
	}
}

func TestSubnegotiationConsumedNotEmitted(t *testing.T) {
	n := NewNegotiator()
	// A terminal-type subnegotiation query: IAC SB TTYPE SEND IAC SE.
	input := []byte{'p', 'r', 'e', IAC, SB, OptTType, 1, IAC, SE, 'p', 'o', 's', 't'}
	out := n.Feed(input)
	if string(out) != "prepost" {
		t.Fatalf("got %q, subnegotiation bytes leaked into application data", out)
	}
}

func TestSubnegotiationWithEscapedIAC(t *testing.T) {
	n := NewNegotiator()
	// Subnegotiation payload containing an escaped 0xFF (IAC IAC) before the
	// terminating IAC SE.
	input := []byte{IAC, SB, OptNAWS, 0x00, IAC, IAC, 0x00, IAC, SE, 'z'}
	out := n.Feed(input)
	if string(out) != "z" {
		t.Fatalf("got %q", out)
	}
}

func TestGoAheadConsumed(t *testing.T) {
	n := NewNegotiator()
	out := n.Feed([]byte{'a', IAC, GA, 'b'})
	if string(out) != "ab" {
		t.Fatalf("got %q", out)
	}
}

func TestNAWSEncoding(t *testing.T) {
	got := NAWS(80, 24)
	want := []byte{IAC, SB, OptNAWS, 0, 80, 0, 24, IAC, SE}
	if !bytes.Equal(got, want) {
		t.Fatalf("NAWS(80,24) = %v, want %v", got, want)
	}
}

func TestNAWSEncodingLargeDimensions(t *testing.T) {
	got := NAWS(300, 100) // cols > 255 exercises the high byte
	want := []byte{IAC, SB, OptNAWS, 1, 44, 0, 100, IAC, SE}
	if !bytes.Equal(got, want) {
		t.Fatalf("NAWS(300,100) = %v, want %v", got, want)
	}
}

func TestWillNAWS(t *testing.T) {
	got := WillNAWS()
	want := []byte{IAC, WILL, OptNAWS}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
