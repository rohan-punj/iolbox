package tool

import (
	"path/filepath"
	"sort"
	"strings"
)

// reapPlanKind identifies one ordered cleanup operation. Keeping the order in
// a portable plan makes stale recovery reviewable without requiring a Linux
// kernel or mocking commands that are deliberately best-effort.
type reapPlanKind string

const (
	reapPlanKillCage   reapPlanKind = "kill-cage"
	reapPlanWaitCage   reapPlanKind = "wait-cage-empty"
	reapPlanRemoveCage reapPlanKind = "remove-cage"
	reapPlanNetns      reapPlanKind = "delete-netns"
	reapPlanVeth       reapPlanKind = "delete-veth"
	reapPlanSocket     reapPlanKind = "remove-socket-dir"
)

// reapPlanEntry is the decision-only representation consumed by the Linux
// cleanup code. A state record is fully destroyed before an unrecorded cage
// leaf is considered, so a crash-recovered record remains the authoritative
// source for netns, veth, and socket ownership.
type reapPlanEntry struct {
	reapKind   reapPlanKind
	reapNodeID int
	reapPath   string
}

// reapStateEligible keeps a foreign state file from turning a cage listing
// into a cross-install deletion request. An absent empty state is eligible for
// the delegated-subtree belt-and-suspenders sweep; an unowned non-empty state
// is not.
func reapStateEligible(state ObjectState, instanceID string) bool {
	if state.InstanceID != "" {
		return state.InstanceID == instanceID
	}
	return len(state.Objects) == 0
}

// reapPlan decides which recorded objects and delegated-cgroup leaves may be
// destroyed. State records are sorted by node ID, each record is cleaned in
// cage, namespace, veth, socket order, and then unrecorded tool-* cage leaves
// are swept in path order. The supervisor leaf is never a sweep candidate.
func reapPlan(state ObjectState, instanceID string, cagePaths []string) []reapPlanEntry {
	if !reapStateEligible(state, instanceID) {
		return nil
	}

	records := make([]ObjectRecord, 0, len(state.Objects))
	for _, record := range state.Objects {
		records = append(records, record)
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].NodeID != records[right].NodeID {
			return records[left].NodeID < records[right].NodeID
		}
		return records[left].CgroupPath < records[right].CgroupPath
	})

	plan := make([]reapPlanEntry, 0, len(records)*6+len(cagePaths)*3)
	recordedCages := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record.CgroupPath != "" {
			recordedCages[record.CgroupPath] = struct{}{}
			plan = append(plan,
				reapPlanEntry{reapKind: reapPlanKillCage, reapNodeID: record.NodeID, reapPath: record.CgroupPath},
				reapPlanEntry{reapKind: reapPlanWaitCage, reapNodeID: record.NodeID, reapPath: record.CgroupPath},
				reapPlanEntry{reapKind: reapPlanRemoveCage, reapNodeID: record.NodeID, reapPath: record.CgroupPath},
			)
		}
		if record.Netns != "" {
			plan = append(plan, reapPlanEntry{reapKind: reapPlanNetns, reapNodeID: record.NodeID})
		}
		if record.HostVeth != "" || record.MgmtVeth != "" {
			plan = append(plan, reapPlanEntry{reapKind: reapPlanVeth, reapNodeID: record.NodeID})
		}
		if record.SocketDir != "" {
			plan = append(plan, reapPlanEntry{reapKind: reapPlanSocket, reapNodeID: record.NodeID, reapPath: record.SocketDir})
		}
	}

	reapExtraCages := make([]string, 0, len(cagePaths))
	for _, cagePath := range cagePaths {
		cageName := filepath.Base(filepath.Clean(cagePath))
		if cageName == SupervisorLeafName || !strings.HasPrefix(cageName, "tool-") {
			continue
		}
		if _, recorded := recordedCages[cagePath]; recorded {
			continue
		}
		reapExtraCages = append(reapExtraCages, cagePath)
	}
	sort.Strings(reapExtraCages)
	for _, cagePath := range reapExtraCages {
		plan = append(plan,
			reapPlanEntry{reapKind: reapPlanKillCage, reapPath: cagePath},
			reapPlanEntry{reapKind: reapPlanWaitCage, reapPath: cagePath},
			reapPlanEntry{reapKind: reapPlanRemoveCage, reapPath: cagePath},
		)
	}
	return plan
}
