package tool

// detectProbeStep is the portable description of one operational capability
// check. Keeping the ordering and fallback explanations here lets callers make
// one stable feature-gate decision even when an individual Linux probe fails.
type detectProbeStep struct {
	key    string
	reason string
}

// detectProbeSteps is the contract between the operational probe and the
// capability matrix. The order is intentionally the dependency order: a
// namespace must exist before its veth can be moved, and both kernel objects
// must exist before a caged transition can be exercised.
var detectProbeSteps = []detectProbeStep{
	{key: "netnsCreate", reason: "tool: netns creation probe failed"},
	{key: "vethCreate", reason: "tool: veth creation probe failed"},
	{key: "vethMoveRename", reason: "tool: veth move/rename probe failed"},
	{key: "cgroupDelegated", reason: "tool: delegated cgroup probe failed"},
	{key: "ambientCapTransition", reason: "tool: ambient capability transition probe failed"},
	{key: "unixProxy", reason: "tool: AF_UNIX bind/dial probe failed"},
}

// detectStepResult is the Linux probe's result for one matrix primitive. The
// reason is retained only for failed primitives so node.start can explain a
// closed feature gate without rerunning the privileged probe.
type detectStepResult struct {
	ok     bool
	reason string
}

// detectCapabilitiesFromResults assembles the public matrix in the same order
// for every platform. Missing results fail closed and receive the pinned
// per-step explanation, which prevents a partial probe from advertising tools.
func detectCapabilitiesFromResults(results map[string]detectStepResult) Capabilities {
	caps := Capabilities{Reasons: make(map[string]string)}
	for _, step := range detectProbeSteps {
		result, present := results[step.key]
		if !present {
			result = detectStepResult{reason: step.reason}
		}
		detectSetCapability(&caps, step.key, result.ok)
		if !result.ok {
			if result.reason == "" {
				result.reason = step.reason
			}
			caps.Reasons[step.key] = result.reason
		}
	}
	return caps
}

// detectSetCapability maps the stable matrix key to the corresponding public
// field. Unknown keys are ignored so a future probe can be staged without
// changing the current feature gate accidentally.
func detectSetCapability(caps *Capabilities, key string, ok bool) {
	switch key {
	case "netnsCreate":
		caps.NetnsCreate = ok
	case "vethCreate":
		caps.VethCreate = ok
	case "vethMoveRename":
		caps.VethMoveRename = ok
	case "cgroupDelegated":
		caps.CgroupDelegated = ok
	case "ambientCapTransition":
		caps.AmbientCapTransition = ok
	case "unixProxy":
		caps.UnixProxy = ok
	}
}
