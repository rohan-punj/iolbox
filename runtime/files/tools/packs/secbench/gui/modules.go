package main

// This file is the single source of truth for the 18 attack/recon modules.
// Every tab, form, start/stop route and mitigation panel is generated from
// this table (see server.go) so adding a module never means hand-rolling a
// new page â€” just another ModuleDef entry + a small python helper.

// Field describes one extra CLI parameter a module's helper accepts, beyond
// the always-present --count/--interval. Rendered as a labelled <input> and
// passed to the helper as "--name value" when non-empty.
type Field struct {
	Name        string // CLI flag name (helper reads --<Name>)
	Label       string
	Placeholder string
	Default     string
	Type        string // "text" | "number"
}

// Mitigation pairs a module with the config that answers it. For the L2/L3
// attack modules that is the exact switch/router config that DEFEATS the
// attack. For the NGFW-test modules it is instead the firewall profile that
// DETECTS/BLOCKS the fired traffic, plus where to watch it in the firewall
// logs â€” same struct, repurposed. Empty Mitigation{} (Config == "") means
// "recon, nothing to show".
type Mitigation struct {
	Title   string
	Config  string // verbatim config block (switch/router IOS, or firewall config)
	Observe string // what to check after applying it
}

// ModuleDef is one attack/recon/test module: label, python helper, its form
// fields, and the paired mitigation/detection panel.
type ModuleDef struct {
	Key        string
	Label      string
	Group      string // tab key: recon | spoof | dhcp | stp | vlan | fhrp
	Script     string // filename under /opt/iolbox/tools/packs/secbench/attacks/
	Blurb      string
	Fields     []Field
	Mitigation Mitigation
}

const (
	groupRecon = "recon"
	groupSpoof = "spoof"
	groupDHCP  = "dhcp"
	groupSTP   = "stp"
	groupVLAN  = "vlan"
	groupFHRP  = "fhrp"
)

var groupOrder = []struct{ Key, Label, Desc string }{
	{groupRecon, "Recon", "Map the segment before attacking it: hosts, DHCP servers, passive traffic."},
	{groupSpoof, "L2 Spoofing", "ARP/MITM, forged source MAC, CAM table overflow."},
	{groupDHCP, "DHCP", "Rogue server and pool-exhaustion (starvation) attacks."},
	{groupSTP, "STP / Discovery", "Root bridge hijack, CDP/LLDP flood-and-spoof."},
	{groupVLAN, "VLAN", "DTP trunk negotiation abuse, 802.1Q double-tag hopping."},
	{groupFHRP, "FHRP / Routing", "HSRP/VRRP hijack, OSPF/EIGRP rogue adjacency, IPv6 RA/DHCPv6 spoof."},
}

var moduleDefs = []ModuleDef{
	// ---- RECON ----
	{
		Key: "arp_scan", Label: "ARP Scan", Group: groupRecon, Script: "arp_scan.py",
		Blurb: "Sweeps a subnet with ARP requests and builds a host/MAC table other modules can target.",
		Fields: []Field{
			{"subnet", "Subnet", "192.168.1.0/24", "", "text"},
		},
	},
	{
		Key: "dhcp_discover", Label: "DHCP Discover", Group: groupRecon, Script: "dhcp_discover.py",
		Blurb: "Broadcasts DHCPDISCOVER and reports every server that answers with an OFFER (finds rogue servers too).",
		Fields: []Field{
			{"duration", "Listen seconds", "10", "10", "number"},
		},
	},
	{
		Key: "sniff", Label: "Passive Sniff", Group: groupRecon, Script: "sniff.py",
		Blurb: "Passively observes traffic on eth1 and summarizes MACs, VLAN tags and top talkers seen.",
		Fields: []Field{
			{"duration", "Duration (s)", "20", "20", "number"},
		},
	},

	// ---- L2 SPOOFING ----
	{
		Key: "arp_spoof", Label: "ARP Spoof / MITM", Group: groupSpoof, Script: "arp_spoof.py",
		Blurb: "Bidirectionally poisons target<->gateway ARP caches to sit in the middle of their traffic.",
		Fields: []Field{
			{"target_ip", "Target IP", "192.168.1.10", "", "text"},
			{"gateway_ip", "Gateway IP", "192.168.1.1", "", "text"},
		},
		Mitigation: Mitigation{
			Title: "Dynamic ARP Inspection + DHCP snooping",
			Config: `ip dhcp snooping
ip dhcp snooping vlan 10
!
interface Gi0/1                 ! uplink to the real DHCP server
 ip dhcp snooping trust
!
ip arp inspection vlan 10
interface Gi0/2                 ! any other trusted/uplink port
 ip arp inspection trust`,
			Observe: "show ip arp inspection statistics vlan 10 â€” Denied ARP count climbs and the forged replies never reach the target; hosts' ARP tables keep the real gateway MAC.",
		},
	},
	{
		Key: "mac_spoof", Label: "MAC Spoof", Group: groupSpoof, Script: "mac_spoof.py",
		Blurb: "Sends traffic from a forged source MAC address to impersonate another host on the segment.",
		Fields: []Field{
			{"spoof_mac", "Source MAC to forge", "02:00:00:aa:bb:cc", "02:00:00:aa:bb:cc", "text"},
			{"target_ip", "Destination IP", "192.168.1.1", "", "text"},
		},
		Mitigation: Mitigation{
			Title: "Port security (sticky, violation restrict)",
			Config: `interface Gi0/3
 switchport mode access
 switchport port-security
 switchport port-security maximum 1
 switchport port-security mac-address sticky
 switchport port-security violation restrict`,
			Observe: "show port-security interface Gi0/3 â€” SecurityViolation count increments and frames with the forged MAC are dropped instead of forwarded.",
		},
	},
	{
		Key: "mac_flood", Label: "CAM / MAC Flood", Group: groupSpoof, Script: "mac_flood.py",
		Blurb:  "macof-style flood of frames with random source MACs to overflow the switch CAM table (fail-open -> flooding).",
		Fields: []Field{},
		Mitigation: Mitigation{
			Title: "Port security (max MAC addresses)",
			Config: `interface range Gi0/1 - 24
 switchport port-security
 switchport port-security maximum 2
 switchport port-security violation shutdown`,
			Observe: "show port-security â€” the flooding port hits its MAC maximum and goes err-disabled instead of the CAM table overflowing and the switch fail-open flooding every VLAN.",
		},
	},

	// ---- DHCP ----
	{
		Key: "dhcp_rogue", Label: "Rogue DHCP Server", Group: groupDHCP, Script: "dhcp_rogue.py",
		Blurb: "Answers DHCPDISCOVER with OFFERs handing out a forged gateway/DNS to redirect victim traffic.",
		Fields: []Field{
			{"pool_start", "Pool start", "192.168.1.100", "192.168.1.100", "text"},
			{"pool_end", "Pool end", "192.168.1.150", "192.168.1.150", "text"},
			{"gateway", "Gateway to hand out", "192.168.1.66", "192.168.1.66", "text"},
			{"dns", "DNS to hand out", "192.168.1.66", "192.168.1.66", "text"},
			{"lease_time", "Lease (s)", "3600", "3600", "number"},
		},
		Mitigation: Mitigation{
			Title: "DHCP snooping + trusted uplink",
			Config: `ip dhcp snooping
ip dhcp snooping vlan 10
interface Gi0/1                 ! uplink to the real DHCP server
 ip dhcp snooping trust`,
			Observe: "show ip dhcp snooping â€” the attacker's port stays untrusted; show ip dhcp snooping statistics shows OFFER/ACK from it dropped instead of reaching clients.",
		},
	},
	{
		Key: "dhcp_starve", Label: "DHCP Starvation", Group: groupDHCP, Script: "dhcp_starve.py",
		Blurb:  "Floods DHCPDISCOVER with random client MACs (chaddr) to exhaust the real server's address pool.",
		Fields: []Field{},
		Mitigation: Mitigation{
			Title: "DHCP snooping rate-limit + port security",
			Config: `interface Gi0/5
 ip dhcp snooping limit rate 15
 switchport port-security
 switchport port-security maximum 3`,
			Observe: "show ip dhcp snooping statistics â€” DISCOVERs from the attacking port are rate-limited/dropped well before the pool exhausts.",
		},
	},

	// ---- STP / DISCOVERY ----
	{
		Key: "stp_root", Label: "STP Root Hijack", Group: groupSTP, Script: "stp_root.py",
		Blurb: "Transmits a superior (lower priority) BPDU to become the STP root bridge and pull traffic through the attacker.",
		Fields: []Field{
			{"priority", "Bridge priority", "0", "0", "number"},
			{"vlan", "VLAN (0=none/PVST off)", "0", "0", "number"},
		},
		Mitigation: Mitigation{
			Title: "BPDU Guard (edge ports) + Root Guard (uplinks)",
			Config: `interface Gi0/10                ! access/edge port
 spanning-tree bpduguard enable
!
interface Gi0/1                 ! port that must never become root-facing
 spanning-tree guard root`,
			Observe: "show spanning-tree summary â€” the edge port goes err-disabled the instant a BPDU arrives (bpduguard), or the uplink goes into root-inconsistent state (root guard) instead of the topology re-converging around the attacker.",
		},
	},
	{
		Key: "cdp_flood", Label: "CDP Flood / Spoof", Group: groupSTP, Script: "cdp_flood.py",
		Blurb: "Floods forged CDP neighbor announcements (device-id/platform) to pollute neighbor tables / exhaust NVRAM.",
		Fields: []Field{
			{"device_id", "Forged device-id", "fake-switch", "fake-switch", "text"},
			{"platform", "Forged platform", "cisco WS-C2960X", "cisco WS-C2960X", "text"},
		},
		Mitigation: Mitigation{
			Title: "Disable CDP",
			Config: `no cdp run
! or, scoped to just the exposed edge port:
interface Gi0/5
 no cdp enable`,
			Observe: "show cdp neighbors â€” stays empty (or unchanged) despite the flood.",
		},
	},
	{
		Key: "lldp_flood", Label: "LLDP Flood / Spoof", Group: groupSTP, Script: "lldp_flood.py",
		Blurb: "Floods forged LLDP TLVs (chassis-id/system-name) â€” the vendor-neutral twin of the CDP flood.",
		Fields: []Field{
			{"chassis_id", "Forged chassis-id", "fake-chassis", "fake-chassis", "text"},
			{"system_name", "Forged system name", "fake-switch", "fake-switch", "text"},
		},
		Mitigation: Mitigation{
			Title: "Disable LLDP",
			Config: `no lldp run
! or, scoped to just the exposed edge port:
interface Gi0/5
 no lldp transmit
 no lldp receive`,
			Observe: "show lldp neighbors â€” stays empty despite the flood.",
		},
	},

	// ---- VLAN ----
	{
		Key: "dtp_hop", Label: "DTP Trunk Hop", Group: groupVLAN, Script: "dtp_hop.py",
		Blurb:  "Forges DTP Desirable frames to negotiate the attached port into trunk mode, exposing every VLAN.",
		Fields: []Field{},
		Mitigation: Mitigation{
			Title: "switchport nonegotiate (static access)",
			Config: `interface Gi0/5
 switchport mode access
 switchport nonegotiate`,
			Observe: "show interfaces Gi0/5 switchport â€” Administrative Mode: static access, Negotiation of Trunking: Off; the port never becomes a trunk no matter how many DTP frames arrive.",
		},
	},
	{
		Key: "vlan_hop", Label: "802.1Q Double-Tag Hop", Group: groupVLAN, Script: "vlan_hop.py",
		Blurb: "Sends a double-tagged frame (outer = native VLAN) that a trunk strips once, hopping the inner VLAN onto the segment.",
		Fields: []Field{
			{"target_vlan", "Inner (target) VLAN", "20", "20", "number"},
			{"native_vlan", "Outer (native) VLAN", "1", "1", "number"},
		},
		Mitigation: Mitigation{
			Title: "Change/park the native VLAN, drop it from the trunk",
			Config: `interface Gi0/1                 ! trunk uplink
 switchport trunk native vlan 999
 switchport trunk allowed vlan remove 999`,
			Observe: "show interfaces trunk â€” native VLAN is now 999 (unused, carries no hosts) and VLAN 1 is pruned, so a double-tagged frame using the old native VLAN no longer hops.",
		},
	},

	// ---- FHRP / ROUTING ----
	{
		Key: "hsrp_hijack", Label: "HSRP Hijack", Group: groupFHRP, Script: "hsrp_hijack.py",
		Blurb: "Sends a higher-priority Coup/Hello to become HSRP Active and intercept the virtual gateway's traffic.",
		Fields: []Field{
			{"group", "HSRP group", "1", "1", "number"},
			{"virtual_ip", "Virtual IP", "192.168.1.1", "192.168.1.1", "text"},
			{"priority", "Forged priority", "255", "255", "number"},
		},
		Mitigation: Mitigation{
			Title: "HSRP MD5 authentication",
			Config: `interface Vlan10
 standby 1 ip 10.0.10.1
 standby 1 priority 110
 standby 1 authentication md5 key-string CHANGEME`,
			Observe: "show standby brief â€” forged hellos without the matching key are rejected outright; no unexpected Active/Speak state churn on the real routers.",
		},
	},
	{
		Key: "vrrp_hijack", Label: "VRRP Hijack", Group: groupFHRP, Script: "vrrp_hijack.py",
		Blurb: "Sends a higher-priority VRRP advertisement to become Master and intercept the virtual router's traffic.",
		Fields: []Field{
			{"vrid", "VRID", "1", "1", "number"},
			{"virtual_ip", "Virtual IP", "192.168.1.1", "192.168.1.1", "text"},
			{"priority", "Forged priority", "255", "255", "number"},
		},
		Mitigation: Mitigation{
			Title: "VRRP authentication",
			Config: `interface Vlan20
 vrrp 1 ip 10.0.20.1
 vrrp 1 priority 110
 vrrp 1 authentication text CHANGEME`,
			Observe: "show vrrp â€” Master stays stable; forged advertisements without the shared secret never trigger a role change.",
		},
	},
	{
		Key: "ospf_rogue", Label: "OSPF Rogue Adjacency", Group: groupFHRP, Script: "ospf_rogue.py",
		Blurb: "Forms (or floods hellos to attempt) a rogue OSPF adjacency to inject/black-hole routes or DoS the process.",
		Fields: []Field{
			{"area", "Area", "0", "0", "number"},
			{"router_id", "Forged router-id", "9.9.9.9", "9.9.9.9", "text"},
		},
		Mitigation: Mitigation{
			Title: "OSPF MD5 authentication + passive-interface default",
			Config: `interface Gi0/1
 ip ospf message-digest-key 1 md5 CHANGEME
 ip ospf authentication message-digest
!
router ospf 1
 area 0 authentication message-digest
 passive-interface default
 no passive-interface Gi0/2      ! only real OSPF links stay active`,
			Observe: "show ip ospf neighbor â€” the rogue speaker never appears; debug ip ospf adj shows hello/auth mismatch drops instead of a new adjacency forming.",
		},
	},
	{
		Key: "eigrp_rogue", Label: "EIGRP Rogue Adjacency", Group: groupFHRP, Script: "eigrp_rogue.py",
		Blurb: "Sends EIGRP hellos to attempt a rogue neighbor relationship on the target AS.",
		Fields: []Field{
			{"asn", "AS number", "100", "100", "number"},
			{"router_id", "Forged router-id", "9.9.9.9", "9.9.9.9", "text"},
		},
		Mitigation: Mitigation{
			Title: "EIGRP MD5 authentication (key chain)",
			Config: `key chain EIGRP-KEYS
 key 1
  key-string CHANGEME
!
interface Gi0/1
 ip authentication mode eigrp 100 md5
 ip authentication key-chain eigrp 100 EIGRP-KEYS`,
			Observe: "show ip eigrp neighbors â€” the rogue router never appears; neighborship fails authentication instead of forming.",
		},
	},
	{
		Key: "ra_spoof", Label: "IPv6 RA / DHCPv6 Spoof", Group: groupFHRP, Script: "ra_spoof.py",
		Blurb: "Sends rogue Router Advertisements (and optional DHCPv6 replies) to redirect IPv6 hosts to a forged default gateway/DNS.",
		Fields: []Field{
			{"prefix", "Advertised prefix", "2001:db8:dead::/64", "2001:db8:dead::/64", "text"},
			{"dns_server", "DNS to advertise (RDNSS)", "2001:db8:dead::1", "2001:db8:dead::1", "text"},
		},
		Mitigation: Mitigation{
			Title: "IPv6 RA Guard + DHCPv6 Guard",
			Config: `ipv6 nd raguard policy HOST-PORT
 device-role host
!
ipv6 dhcp guard policy DHCPV6-HOST
 device-role client
!
interface Gi0/5
 ipv6 nd raguard attach-policy HOST-PORT
 ipv6 dhcp guard attach-policy DHCPV6-HOST`,
			Observe: "show ipv6 nd raguard policy HOST-PORT â€” dropped-RA counters increment on the port; hosts keep the legitimate default gateway/DHCPv6 lease instead of the rogue one.",
		},
	},
}

func moduleByKey(key string) *ModuleDef {
	for i := range moduleDefs {
		if moduleDefs[i].Key == key {
			return &moduleDefs[i]
		}
	}
	return nil
}

func modulesInGroup(group string) []ModuleDef {
	var out []ModuleDef
	for _, m := range moduleDefs {
		if m.Group == group {
			out = append(out, m)
		}
	}
	return out
}
