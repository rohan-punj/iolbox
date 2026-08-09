//go:build !linux

package tool

import "time"

const (
	endpointReadinessTimeout = 10 * time.Second
	endpointProbeInterval    = 100 * time.Millisecond
	endpointLivenessInterval = 5 * time.Second
	endpointLivenessFailures = 3
	endpointStopTimeout      = 5 * time.Second
)

// Endpoint is a placeholder off Linux so server signatures resolve without
// pretending that namespaces, cgroups, or AF_UNIX launch are available.
type Endpoint struct{}

// Start refuses to create a partial tool endpoint off Linux.
func Start(cfg Config) (*Endpoint, error) { return nil, ErrUnsupportedPlatform }

// Stop is a no-op off Linux.
func (e *Endpoint) Stop() error { return nil }

// AttachBridge is a no-op off Linux.
func (e *Endpoint) AttachBridge(br string) error { return nil }

// DetachBridge is a no-op off Linux.
func (e *Endpoint) DetachBridge() {}

// State has no lifecycle on an unsupported platform.
func (e *Endpoint) State() string { return "" }

// PID has no child process on an unsupported platform.
func (e *Endpoint) PID() int { return 0 }

// HostVeth has no network device on an unsupported platform.
func (e *Endpoint) HostVeth() string { return "" }

// SocketPath has no AF_UNIX endpoint on an unsupported platform.
func (e *Endpoint) SocketPath() string { return "" }

func endpointOptionsPayload(options []byte) []byte {
	if len(options) == 0 {
		return []byte("{}")
	}
	return append([]byte(nil), options...)
}

func endpointSetupSteps() []string {
	return []string{"cgroup", "netns", "veth"}
}

func endpointTeardownSteps() []string {
	setup := endpointSetupSteps()
	steps := make([]string, len(setup))
	for index := range setup {
		steps[len(setup)-index-1] = setup[index]
	}
	return steps
}

func endpointReadinessFlip(statuses []int) bool {
	return len(statuses) > 0 && statuses[len(statuses)-1] == 200
}

func endpointLivenessTrip(statuses []int) bool {
	if len(statuses) < endpointLivenessFailures {
		return false
	}
	for _, status := range statuses[len(statuses)-endpointLivenessFailures:] {
		if status == 200 {
			return false
		}
	}
	return true
}
