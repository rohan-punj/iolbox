package tool

import (
	"reflect"
	"testing"
)

func TestReapPlanRecordedObjectsOrder(t *testing.T) {
	state := ObjectState{
		InstanceID: "install-a",
		Objects: map[string]ObjectRecord{
			"9": {NodeID: 9, CgroupPath: "/delegated/tool-9", Netns: "iolt9", HostVeth: "vtool9", SocketDir: "/run/tool/9"},
			"2": {NodeID: 2, CgroupPath: "/delegated/tool-2", Netns: "iolt2", HostVeth: "vtool2", MgmtVeth: "mtool2", SocketDir: "/run/tool/2"},
		},
	}
	got := reapPlan(state, "install-a", []string{"/delegated/tool-9", "/delegated/tool-4", "/delegated/supervisor"})
	want := []reapPlanEntry{
		{reapKind: reapPlanKillCage, reapNodeID: 2, reapPath: "/delegated/tool-2"},
		{reapKind: reapPlanWaitCage, reapNodeID: 2, reapPath: "/delegated/tool-2"},
		{reapKind: reapPlanRemoveCage, reapNodeID: 2, reapPath: "/delegated/tool-2"},
		{reapKind: reapPlanNetns, reapNodeID: 2},
		{reapKind: reapPlanVeth, reapNodeID: 2},
		{reapKind: reapPlanSocket, reapNodeID: 2, reapPath: "/run/tool/2"},
		{reapKind: reapPlanKillCage, reapNodeID: 9, reapPath: "/delegated/tool-9"},
		{reapKind: reapPlanWaitCage, reapNodeID: 9, reapPath: "/delegated/tool-9"},
		{reapKind: reapPlanRemoveCage, reapNodeID: 9, reapPath: "/delegated/tool-9"},
		{reapKind: reapPlanNetns, reapNodeID: 9},
		{reapKind: reapPlanVeth, reapNodeID: 9},
		{reapKind: reapPlanSocket, reapNodeID: 9, reapPath: "/run/tool/9"},
		{reapKind: reapPlanKillCage, reapPath: "/delegated/tool-4"},
		{reapKind: reapPlanWaitCage, reapPath: "/delegated/tool-4"},
		{reapKind: reapPlanRemoveCage, reapPath: "/delegated/tool-4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reapPlan = %#v, want %#v", got, want)
	}
}

func TestReapPlanForeignStateDestroysNothing(t *testing.T) {
	state := ObjectState{
		InstanceID: "install-b",
		Objects: map[string]ObjectRecord{
			"7": {NodeID: 7, CgroupPath: "/delegated/tool-7", Netns: "iolt7", HostVeth: "vtool7"},
		},
	}
	if got := reapPlan(state, "install-a", []string{"/delegated/tool-7", "/delegated/tool-8", "/delegated/supervisor"}); got != nil {
		t.Fatalf("foreign-state plan = %#v, want no cleanup", got)
	}
}

func TestReapPlanSweepsUnrecordedToolLeavesOnly(t *testing.T) {
	cages := []string{
		"/delegated/tool-12",
		"/delegated/supervisor",
		"/delegated/tool-3",
		"/delegated/supervisor-2",
		"/delegated/tool-invalid",
	}
	got := reapPlan(ObjectState{}, "install-a", cages)
	want := []reapPlanEntry{
		{reapKind: reapPlanKillCage, reapPath: "/delegated/tool-12"},
		{reapKind: reapPlanWaitCage, reapPath: "/delegated/tool-12"},
		{reapKind: reapPlanRemoveCage, reapPath: "/delegated/tool-12"},
		{reapKind: reapPlanKillCage, reapPath: "/delegated/tool-3"},
		{reapKind: reapPlanWaitCage, reapPath: "/delegated/tool-3"},
		{reapKind: reapPlanRemoveCage, reapPath: "/delegated/tool-3"},
		{reapKind: reapPlanKillCage, reapPath: "/delegated/tool-invalid"},
		{reapKind: reapPlanWaitCage, reapPath: "/delegated/tool-invalid"},
		{reapKind: reapPlanRemoveCage, reapPath: "/delegated/tool-invalid"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unrecorded-cage plan = %#v, want %#v", got, want)
	}
}
