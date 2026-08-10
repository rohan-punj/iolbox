package tool

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

func TestEndpointReadinessFlipsOnlyOnHealth200(t *testing.T) {
	if endpointReadinessTimeout != 10*time.Second || endpointProbeInterval != 100*time.Millisecond {
		t.Fatalf("readiness cadence = timeout %s, poll %s; want 10s/100ms", endpointReadinessTimeout, endpointProbeInterval)
	}
	if endpointReadinessFlip([]int{404}) {
		t.Fatal("readiness flipped on a 404")
	}
	if !endpointReadinessFlip([]int{0, 503, 200}) {
		t.Fatal("readiness did not flip on a 200")
	}
}

func TestEndpointLivenessNeedsThreeConsecutiveFailures(t *testing.T) {
	if endpointLivenessInterval != 5*time.Second || endpointLivenessFailures != 3 {
		t.Fatalf("liveness cadence = interval %s, failures %d; want 5s/3", endpointLivenessInterval, endpointLivenessFailures)
	}
	if endpointLivenessTrip([]int{500, 500}) {
		t.Fatal("liveness tripped after two failures")
	}
	if endpointLivenessTrip([]int{500, 500, 200, 500, 500}) {
		t.Fatal("liveness ignored a successful reset")
	}
	if !endpointLivenessTrip([]int{500, 404, 0}) {
		t.Fatal("liveness did not trip after three non-200 probes")
	}
}

func TestEndpointTeardownIsReverseOfSetup(t *testing.T) {
	setup := endpointSetupSteps()
	teardown := endpointTeardownSteps()
	want := make([]string, len(setup))
	for index := range setup {
		want[len(setup)-index-1] = setup[index]
	}
	if !reflect.DeepEqual(teardown, want) {
		t.Fatalf("teardown steps = %#v, want reverse %#v", teardown, want)
	}
}

func TestEndpointOptionsPayloadDefaultsToEmptyObject(t *testing.T) {
	if got := endpointOptionsPayload(nil); !bytes.Equal(got, []byte("{}")) {
		t.Fatalf("nil options = %q, want {}", got)
	}
	if got := endpointOptionsPayload([]byte{}); !bytes.Equal(got, []byte("{}")) {
		t.Fatalf("empty options = %q, want {}", got)
	}
	original := []byte(`{"mode":"safe"}`)
	got := endpointOptionsPayload(original)
	if !bytes.Equal(got, original) {
		t.Fatalf("options = %q, want %q", got, original)
	}
	got[0] = 'X'
	if original[0] == 'X' {
		t.Fatal("options payload aliases caller storage")
	}
}
