package tool

import (
	"reflect"
	"testing"
)

func TestNetnsCreateSequence(t *testing.T) {
	got := append(netnsCreateNetnsCmds(7), netnsCreateVethCmds(7)...)
	want := []cmdSpec{
		{name: "ip", args: []string{"netns", "add", "iolt7"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "sysctl", "-w", "net.ipv4.ip_unprivileged_port_start=1"}},
		{name: "ip", args: []string{"link", "add", "vtool7", "type", "veth", "peer", "name", "vtoolp7"}},
		{name: "ip", args: []string{"link", "set", "vtoolp7", "netns", "iolt7"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "ip", "link", "set", "vtoolp7", "name", "eth1"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "ip", "link", "set", "eth1", "up"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "ip", "link", "set", "lo", "up"}},
		{name: "ip", args: []string{"link", "set", "vtool7", "up"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("netns setup argv = %#v, want %#v", got, want)
	}

	for _, spec := range got {
		for _, arg := range spec.args {
			if arg != GuestIface {
				continue
			}
			if len(spec.args) < 4 || spec.args[0] != "netns" || spec.args[1] != "exec" {
				t.Fatalf("eth1 escaped the namespace prefix in %#v", spec.args)
			}
		}
	}
}

func TestNetnsCreateSysctlIsNamespacePrefixed(t *testing.T) {
	cmds := netnsCreateNetnsCmds(11)
	if len(cmds) < 2 {
		t.Fatalf("netns creation commands = %#v, want namespace creation and sysctl commands", cmds)
	}

	args := cmds[1].args
	if cmds[1].name != "ip" || len(args) < 4 || args[0] != "netns" || args[1] != "exec" || args[2] != NetnsName(11) || args[3] != "sysctl" {
		t.Fatalf("sysctl command is not namespace-prefixed: %#v", cmds[1])
	}
}

func TestEndpointSetupStepsExcludeNetnsSysctl(t *testing.T) {
	want := []string{"cgroup", "netns", "veth"}
	if got := endpointSetupSteps(); !reflect.DeepEqual(got, want) {
		t.Fatalf("endpoint setup steps = %#v, want %#v", got, want)
	}
}

func TestNetnsBridgeAndDeleteCommands(t *testing.T) {
	checks := []struct {
		name string
		got  []cmdSpec
		want []cmdSpec
	}{
		{
			name: "attach",
			got:  netnsAttachVethCmds(7, "iolbr9"),
			want: []cmdSpec{{name: "ip", args: []string{"link", "set", "vtool7", "master", "iolbr9"}}},
		},
		{
			name: "detach",
			got:  netnsDetachVethCmds(7),
			want: []cmdSpec{{name: "ip", args: []string{"link", "set", "vtool7", "nomaster"}}},
		},
		{
			name: "delete netns",
			got:  netnsDeleteNetnsCmds(7),
			want: []cmdSpec{{name: "ip", args: []string{"netns", "del", "iolt7"}}},
		},
		{
			name: "delete veth",
			got:  netnsDeleteVethCmds(7),
			want: []cmdSpec{{name: "ip", args: []string{"link", "del", "vtool7"}}},
		},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if !reflect.DeepEqual(check.got, check.want) {
				t.Fatalf("argv = %#v, want %#v", check.got, check.want)
			}
		})
	}
}

func TestNetnsMgmtCommands(t *testing.T) {
	hostCIDR, guestCIDR, err := netnsMgmtCIDRs(7)
	if err != nil {
		t.Fatal(err)
	}
	if hostCIDR != "169.254.7.0/31" || guestCIDR != "169.254.7.1/31" {
		t.Fatalf("management CIDRs = %q, %q", hostCIDR, guestCIDR)
	}

	got := netnsSetupMgmtCmds(7)
	want := []cmdSpec{
		{name: "ip", args: []string{"link", "add", "mtool7", "type", "veth", "peer", "name", "mtoolp7"}},
		{name: "ip", args: []string{"link", "set", "mtoolp7", "netns", "iolt7"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "ip", "link", "set", "mtoolp7", "name", "mgmt0"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "ip", "link", "set", "mgmt0", "up"}},
		{name: "ip", args: []string{"link", "set", "mtool7", "up"}},
		{name: "ip", args: []string{"addr", "add", "169.254.7.0/31", "dev", "mtool7"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "ip", "addr", "add", "169.254.7.1/31", "dev", "mgmt0"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-P", "INPUT", "DROP"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-P", "OUTPUT", "DROP"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-P", "FORWARD", "DROP"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-A", "INPUT", "-i", "lo", "-j", "ACCEPT"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-A", "OUTPUT", "-o", "lo", "-j", "ACCEPT"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-A", "INPUT", "-i", "mgmt0", "-s", "169.254.7.0/31", "-d", "169.254.7.1/31", "-j", "ACCEPT"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-A", "OUTPUT", "-o", "mgmt0", "-s", "169.254.7.1/31", "-d", "169.254.7.0/31", "-j", "ACCEPT"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-A", "FORWARD", "-i", "mgmt0", "-o", "eth1", "-j", "DROP"}},
		{name: "ip", args: []string{"netns", "exec", "iolt7", "iptables", "-A", "FORWARD", "-i", "eth1", "-o", "mgmt0", "-j", "DROP"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("management setup argv = %#v, want %#v", got, want)
	}

	for _, spec := range got {
		for _, arg := range spec.args {
			if arg == GuestIface || arg == netnsMgmtIface {
				if len(spec.args) < 4 || spec.args[0] != "netns" || spec.args[1] != "exec" {
					t.Fatalf("namespace interface escaped the namespace prefix in %#v", spec.args)
				}
			}
		}
	}

	teardown := netnsTeardownMgmtCmds(7)
	if len(teardown) != 12 {
		t.Fatalf("management teardown has %d commands, want 12: %#v", len(teardown), teardown)
	}
	if !reflect.DeepEqual(teardown[len(teardown)-1], cmdSpec{name: "ip", args: []string{"link", "del", "mtool7"}}) {
		t.Fatalf("management teardown must delete the host-side veth last: %#v", teardown[len(teardown)-1])
	}
}

func TestNetnsMgmtCIDRRejectsUnrepresentableNode(t *testing.T) {
	if _, _, err := netnsMgmtCIDRs(256); err == nil {
		t.Fatal("node id 256 must not wrap onto another management /31")
	}
}
