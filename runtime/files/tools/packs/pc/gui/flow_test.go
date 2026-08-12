package main

import "testing"

func TestFlowCapsRejectInsteadOfClamp(t *testing.T) {
	m := NewFlowManager()
	base := FlowSpec{Protocol: "udp", Host: "127.0.0.1", Port: 9, PPS: 1, Bytes: 1, Seconds: 1}
	if _, err := m.Start(FlowSpec{Protocol: "udp", Host: "127.0.0.1", Port: 9, PPS: maxFlowPPS + 1, Bytes: 1, Seconds: 1}); err == nil {
		t.Fatal("over-cap PPS accepted")
	}
	if _, err := m.Start(FlowSpec{Protocol: "udp", Host: "127.0.0.1", Port: 9, PPS: 1, Bytes: maxFlowBytes + 1, Seconds: 1}); err == nil {
		t.Fatal("over-cap payload accepted")
	}
	for i := 0; i < maxFlows; i++ {
		if _, err := m.Start(base); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Start(base); err == nil {
		t.Fatal("fifth flow accepted")
	}
}
