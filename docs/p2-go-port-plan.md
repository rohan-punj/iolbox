# secbench Go port — remaining 16 modules (REVISION 2, post sol-medium review)

Spike (`tools/secbench-attacks-go`) proved wire-equivalence for arp_spoof and
cdp_flood: byte-identical frames vs. the real Python/scapy scripts, verified
on the appliance VM. This plan ports the remaining 16 modules using the same
package (`internal/attackcommon`, `cmd/<module>/main.go`), same conventions,
same discipline (real scapy source as ground truth, no wire-format-from-memory).

**Revision 2 changes** (from sol-medium's adversarial review of rev 1 — 18
findings, all addressed below): fixed two already-shipped log-format bugs in
`attackcommon.go` (done, see below); moved `BuildDHCPDiscover` fully into
Batch 0 (rev 1 had it conditionally in Batch 2, which broke Batch 3's
independence); added `InterfaceIPv4`, `RawReceiver.Close`, VLAN-aware frame
parsing, and an ICMP/IPv4-defaults spec to Batch 0; corrected the DTP SNAP
OUI (was wrongly stated as 0), the HSRP/VRRP/EIGRP/OSPF checksum claims (each
protocol's checksum scope differs — see the per-module byte specs below, not
a vague "each has its own algorithm"); pinned exact BOOTP/DHCP byte layout;
inventoried every Python field left to scapy's implicit auto-resolution (not
just HSRP/VRRP) and made an explicit call for each; tightened the
integration gate to byte-diff every outbound builder, not a sample, with
exact normalization rules for random/intentionally-deterministic fields.

## Fixed already (this revision, before any new code lands)

`internal/attackcommon/attackcommon.go`'s `EnforceLabIface` and `SelftestOK`
did not match Python's exact stdout text (Python uses `%r` → single-quoted,
em dash `—`; Go used `%q` → double-quoted, hyphen `-`). Fixed to emit
`'iface'` and `—` matching Python byte-for-byte for every interface name this
codebase actually uses (no embedded quotes/backslashes to escape). Verified
`go build`/`vet`/`test` still pass. This must NOT be redone by any batch.

## Reference sources

`C:\Users\WS-HOME\AppData\Local\Temp\claude\J--Claude-code\62731f7a-6bab-42ef-8739-10a508e53f11\scratchpad\go-spike-refs\`:
- `scapy-dtp.py`, `scapy-eigrp.py`, `scapy-hsrp.py`, `scapy-vrrp.py` (full files)
- `trim-l2.py` (STP, Dot1Q, Dot3, LLC, SNAP, BCDFloatField, OUIField)
- `trim-dhcp.py` (BOOTP, DHCP, DHCPOptionsField, dhcp_options code table)
- `trim-lldp.py` (the 5 LLDP TLV classes used)
- `trim-ospf.py` (OSPF_Hdr, OSPF_Hello)
- `trim-inet6.py` (ICMPv6ND_RA + 3 ND options + in6_chksum signature)
- `trim-dhcp6.py` (DHCP6_Advertise/Solicit, 2 options, DUID_LLT — NOTE: the
  Python script does NOT use this class; see ra_spoof spec below, it embeds
  10 raw DUID bytes directly, no DUID parsing/construction needed)

Byte-level facts below (checksum scopes, field layouts, defaults) were
pulled directly from these files by the orchestrator and cross-checked
against scapy's actual `post_build` methods — they are not restatements of
the plan's own claims, they ARE the ground truth. Every agent must still
read the cited files itself for anything not spelled out here (e.g. exact
`fields_desc` ordering) — this document does not replace that.

Every agent MUST also read the Python script(s) it's porting
(`runtime/files/tools/packs/secbench/attacks/<name>.py`) for CLI flags, exact
log message text, `--selftest` behavior, and `run_loop`/`enforce_lab_iface`
semantics (already implemented in `internal/attackcommon`).

## Scapy IPv4 defaults (needed by every IP-layer builder)

A bare `IP(...)` in scapy defaults to: `version=4, ihl=None(→5, no options),
tos=0, id=1, flags=0, frag=0, ttl=64, proto=<auto from next layer>,
chksum=None(→computed), src=<auto-resolved from routing table unless set>,
dst=<must be set>`. None of the 16 scripts override `id`, `tos`, `flags`, or
`ttl`, so `BuildIPv4` must hardcode `id=1, tos=0, flags=0, fragOffset=0,
ttl=64` — not zero/Go-idiomatic defaults. `proto` is always explicit in every
script that needs it (BOOTP/DHCP=17/UDP, EIGRP=88, OSPF=89, VRRP=112,
ICMP=1) so `BuildIPv4` takes proto as a parameter, never infers it.

## Every place a Python script leaves a field to scapy's implicit resolution

(Full inventory — rev 1 only caught HSRP/VRRP. Decision recorded for each;
"deterministic" means the Go port sets it explicitly from the interface's
real MAC/IP via `attackcommon`, "random" means matches Python's own
randomization, "n/a" means the Python sets it explicitly so there is no
ambiguity.)

| Module | Field | Python behavior | Go port decision |
|---|---|---|---|
| hsrp_hijack | IP.src (dst 224.0.0.2) | scapy routing-table auto-resolve | **deterministic**: `InterfaceIPv4(eth1)` — documented improvement, see below |
| vrrp_hijack | IP.src (dst 224.0.0.18) | same | **deterministic**: `InterfaceIPv4(eth1)` |
| mac_spoof | IP.src (dst target_ip or broadcast) | same | **deterministic**: `InterfaceIPv4(eth1)` — same rationale, keep consistent |
| vlan_hop | IP.src (dst broadcast) | same | **deterministic**: `InterfaceIPv4(eth1)` |
| ra_spoof (RA) | Ether.src | unset → scapy resolves via `srp`/routing, effectively the real iface MAC when sent with `sendp(iface=...)` | **deterministic**: `IfaceMAC(eth1)` (reuse existing `iface_mac`-equivalent already in the spike, e.g. same helper `arp_spoof`/`vlan_hop` use) |
| ra_spoof (RA) | IPv6.src | unset → scapy resolves the interface's link-local address | **deterministic**: read eth1's link-local `fe80::/10` address at runtime (needed anyway for the ICMPv6 checksum's pseudo-header source field) — if none is configured, fall back to `::` and log same as Python would (scapy would also emit `::` if it can't resolve one) |
| ra_spoof (DHCP6 Advertise reply) | Ether.src, IPv6.src | unset, same auto-resolve | **deterministic**: same as above (reuse the resolved values) |
| eigrp_rogue, ospf_rogue | IP.src | **n/a** — Python sets `src=router_id` explicitly | copy `--router_id` verbatim, no ambiguity |
| dhcp_discover, dhcp_rogue, arp_scan | Ether/IP src | **n/a** — Python calls `iface_mac`/`iface_ipv4` explicitly | same helpers, already exist |

Document this table's "deterministic" rows as intentional, reasoned
improvements in code comments (one line each) — do not silently diverge
without saying so, and do not treat it as a bug to hide from the user.

## Batch 0 — shared primitives (serial, must land first, blocks everything else)

One agent extends `internal/attackcommon` ONLY (no `cmd/` changes):

1. **`BuildIPv4(src, dst net.IP, proto byte, payload []byte) []byte`** — 20-byte
   header (no options), fields per the defaults table above, RFC 791
   ones-complement checksum. Golden test: build one known IP/UDP payload and
   assert exact byte output (don't just checksum-round-trip).
2. **`BuildUDP(src, dst net.IP, sport, dport uint16, payload []byte, ipv6 bool) []byte`**
   — for IPv4, compute the checksum the same way scapy does (it always
   computes UDP checksum unless explicitly zeroed — none of these scripts
   zero it, so always compute). For IPv6 use `in6_chksum`'s pseudo-header
   algorithm (protocol 17, src+dst+length+zero-pad+next-header, see
   `trim-inet6.py`'s `in6_chksum` for the exact composition it delegates to
   `in6_pseudoheader` — since that function's source isn't in the trim,
   replicate RFC 2460 §8.1 directly: pseudo-header = IPv6 src(16) + dst(16)
   + upper-layer-length(4, big-endian) + zero(3) + next-header(1), then
   ones-complement checksum over pseudo-header + UDP header + payload).
3. **`BuildICMPEcho(id, seq uint16, payload []byte) []byte`** — type=8 code=0,
   RFC 792 ones-complement checksum over the ICMP header+payload. scapy's
   bare `ICMP()` defaults to id=0, seq=0, no payload — match that exactly
   for mac_spoof/vlan_hop's `IP(...)/ICMP()` (no explicit id/seq/payload set
   in either script).
4. **`InterfaceIPv4(iface string) net.IP`** — the lab NIC's own IPv4, or
   `0.0.0.0` if unset (mirrors Python's `iface_ipv4`, used both for arp_scan
   parity and the new deterministic-source decisions above).
5. **`BuildDHCPDiscover(xid uint32, chaddr [6]byte, srcMAC net.HardwareAddr) []byte`**
   — the full frame (Ether/IP/UDP/BOOTP/DHCP), used identically by
   dhcp_discover AND dhcp_starve. Put this here, not conditionally in a
   later batch — both cmd/ ports depend on it and must not diverge.
   BOOTP layout (236 bytes fixed, per `trim-dhcp.py`): op(1)=1 htype(1)=1
   hlen(1)=6 hops(1)=0 xid(4) secs(2)=0 flags(2)=0x8000 ciaddr(4)=0.0.0.0
   yiaddr(4)=0.0.0.0 siaddr(4)=0.0.0.0 giaddr(4)=0.0.0.0 chaddr(16, the 6
   real bytes + 10 zero bytes of padding) sname(64 zero bytes)
   file(128 zero bytes), then DHCP magic cookie `63:82:53:63` (4 bytes),
   then options: `(53,1,[1])` message-type=discover, `255` end (no padding
   after end — scapy does not pad DHCP options beyond the end marker for
   this option set). Total frame = 14 (Ether) + 20 (IP) + 8 (UDP) + 236
   (BOOTP) + 4 (magic) + 3 (opt 53) + 1 (opt 255 end) bytes.
6. **`BuildDHCPOffer(xid uint32, chaddr [6]byte, offerIP, gateway, dns net.IP, leaseTime uint32, srcMAC net.HardwareAddr) []byte`**
   for dhcp_rogue: same BOOTP shape but `op=2`, `yiaddr=offerIP`,
   `siaddr=gateway`, `chaddr` copied verbatim from the received DISCOVER
   (not regenerated), options in this EXACT order (scapy serializes the
   options list in the order given, do not reorder):
   `(53,1,[2])` offer, `(54,4,gateway)` server_id, `(51,4,leaseTime)`
   lease_time, `(3,4,gateway)` router, `(6,4,dns)` name_server,
   `(1,4,255.255.255.0)` subnet_mask, `255` end.
7. **`type RawReceiver struct{...}`**, `OpenRawReceiver(iface string) (*RawReceiver, error)`,
   `SetReadDeadline`, `ReadFrame() ([]byte, error)`, **and `Close() error`**
   (rev 1 omitted Close — every receive-path cmd/ must release the socket).
   No BPF compilation; callers filter by parsing the frame themselves.
8. **Minimal frame parsers**, VLAN-aware (rev 1 missed this: a tagged frame's
   L3 offset shifts by 4 bytes per 802.1Q tag, and `sniff` specifically needs
   to detect single or double tags to report `Dot1Q.vlan`):
   - `ParseEthernet(frame []byte) (dstMAC, srcMAC net.HardwareAddr, ethertype uint16, vlanTags []uint16, payload []byte)` — peel any stacked `0x8100` tags, return the VLAN ID(s) seen and the payload starting at the real ethertype/next-header.
   - `ParseARP(payload []byte) (op uint16, srcMAC net.HardwareAddr, srcIP net.IP, dstIP net.IP, ok bool)`.
   - `ParseIPv4UDP(payload []byte) (srcIP, dstIP net.IP, sport, dport uint16, udpPayload []byte, ok bool)`.
   - `ParseBOOTP(udpPayload []byte) (xid uint32, chaddr [6]byte, yiaddr net.IP, msgType byte, ok bool)` — msgType is DHCP option 53's value (1=DISCOVER, 2=OFFER), extracted by scanning the option TLVs after the magic cookie.
   - `ParseDHCP6(udpPayload []byte) (msgType byte, trid [3]byte, clientIDOpt []byte, ok bool)` — `clientIDOpt` is the **raw, unparsed bytes of option 1 (Client-ID) including its type+len+duid**, needed so ra_spoof can copy it byte-for-byte into the Advertise reply exactly like the Python does (`cid = solicited_pkt.getlayer(DHCP6OptClientId)`); do not attempt to decode/reconstruct the DUID.
9. **`HostsInCIDR(cidr string) ([]net.IP, error)`** — matches scapy's `Net()`
   expansion of an ARP `pdst` string EXACTLY: scapy's `Net` iterates every
   address in the range **including the network and broadcast addresses**
   (it has no concept of "host bits" like a routing library would — it's a
   flat address-range iterator). Return that full inclusive range. Guard
   against pathological input: return an error for prefix lengths shorter
   than /22 (1024+ addresses) rather than silently allocating — arp_scan's
   `--subnet` is operator-supplied in a lab context, a hard cap here is a
   safety rail, not a functional requirement, so document it as such.

Add unit tests for `BuildIPv4`/`BuildUDP`/`BuildICMPEcho` checksums against
known-good payloads (same style as `TestCDPChecksumKnownPayload`), for
`BuildDHCPDiscover`/`BuildDHCPOffer` against a hand-computed golden frame,
and for `HostsInCIDR` including a /31 and /32 edge case.

Run `GOOS=linux CGO_ENABLED=0 go build ./... && go vet ./... && go test ./...`
before handing off to batches 1-4 — this batch blocks everything else and
batches 1-4 must NOT edit `internal/attackcommon` at all (no exceptions this
revision — the one exception in rev 1 is removed).

## Batches 1-4 — cmd/ ports (parallel, after batch 0 lands and is verified)

Each agent owns ONLY its listed `cmd/<name>/` directories, read-only against
`internal/attackcommon`.

**Batch 1 — L2/simple-IP sends, 5 modules:** mac_flood, mac_spoof, stp_root,
vlan_hop, dtp_hop.
- mac_flood: `Ether(src=rand,dst=rand)/IP(src="10.x.x.x" random,dst=255.255.255.255,proto=IP-default-for-Raw=0/actually scapy leaves proto to default when payload is Raw — check `IP()/Raw(...)` in a live scapy REPL is not available here, so instead: build `BuildIPv4` with `proto=0` is WRONG — scapy's default `IP().proto` when the next layer isn't a recognized bind (Raw) is **`0` (hopopt/IP)**; confirm by reading `scapy.layers.inet.IP`'s `proto` field default (`ByteEnumField("proto", 0, ...)`) — it is 0 unless a bound next-layer overrides it, and Raw has no binding, so proto=0 here.) / `Raw(b"pnet-secbench-mac-flood")`.
- mac_spoof: `Ether(src=spoof_mac,dst=ff:ff:ff:ff:ff:ff)/IP(src=InterfaceIPv4(eth1),dst=target_ip-or-255.255.255.255,proto=1)/ICMP()` via `BuildICMPEcho(0,0,nil)`.
- vlan_hop: `Ether(src=IfaceMAC,dst=broadcast)/Dot1Q(vlan=native,type=0x8100)/Dot1Q(vlan=target,type=0x0800)/IP(src=InterfaceIPv4,dst=255.255.255.255,proto=1)/ICMP()`. Per `trim-l2.py`, `Dot1Q.type` is the EtherType of the NEXT layer — the outer tag's type is `0x8100` (802.1Q, because the next layer is another Dot1Q), the inner tag's type is `0x0800` (IPv4, because the next layer is IP). Do not leave either at the zero default.
- stp_root: `Dot3(dst=01:80:c2:00:00:00,src=bridge_mac,len=auto)/LLC(dsap=0x42,ssap=0x42,ctrl=3)/STP(...)`, or with `Dot1Q(vlan=vlan,type=<LLC's ethertype, i.e. ≤1500 so scapy treats next layer as LLC — set to the actual serialized LLC+STP length, not a fixed ethertype>)` when `--vlan` != 0 — read `trim-l2.py`'s `Dot1Q.default_payload_class`/`extract_padding` carefully: for an LLC-bearing Dot1Q, `type` holds the LLC+payload **length**, not an EtherType. `STP` fields: `rootid(2)=priority, rootmac(6)=bridge_mac, pathcost(4)=0, bridgeid(2)=priority, bridgemac(6)=bridge_mac, portid(2)=0x8001, age=0(default,unset)→0x0000, maxage=20→0x1400, hellotime=2→0x0200, fwddelay=15→0x0F00` (BCDFloatField = `int(x*256)` big-endian 16-bit, confirmed in `trim-l2.py`). `Dot3.len` is scapy-computed = total payload length after the 14-byte Dot3 header.
- dtp_hop: `Dot3(src=my_mac,dst=01:00:0c:cc:cc:cc)/LLC(dsap=0xAA,ssap=0xAA,ctrl=3)/SNAP(OUI=0x00000c,code=0x2004)/DTP(...)` — **corrected from rev 1**: the SNAP OUI is `0x00000c` (Cisco), NOT zero, and the PID/code is `0x2004` (DTP), confirmed directly in `scapy-dtp.py`'s `bind_layers(SNAP, DTP, ...)`/overload — read that file for the exact TLV type codes for DTPDomain/DTPStatus/DTPType/DTPNeighbor and their field layouts.

**Batch 2 — IPv4-layer sends with protocol-specific checksums, 5 modules:**
lldp_flood, eigrp_rogue, ospf_rogue, hsrp_hijack, vrrp_hijack.
- lldp_flood: `Ether(dst=01:80:c2:00:00:0e,src=forgedMAC,type=0x88cc)/` 5 LLDP TLVs per `trim-lldp.py`'s exact bit-packed 7-bit-type + 9-bit-length header (NOT byte-aligned like CDP/DTP — this is the one place in this wave where getting bit-packing wrong is easy). **Frame padding**: scapy pads any Ethernet frame's total wire length up to 60 bytes minimum (excluding the 4-byte FCS, which neither scapy nor this Go stack adds) — if the 5 TLVs serialize to fewer than 46 bytes of payload, both scapy and the Go port must zero-pad to 60 bytes total frame length. Verify against a live capture rather than assuming; this is exactly the kind of byte the spike's masking discipline is meant to catch.
- eigrp_rogue: `Ether(src=IfaceMAC,dst=01:00:5e:00:00:0a)/BuildIPv4(src=router_id,dst=224.0.0.10,proto=88,payload=eigrpMsg)`. EIGRP header (per `scapy-eigrp.py` lines ~460-480): `ver(1)=2 opcode(1)=5 chksum(2) flags(4)=0 seq(4)=0 ack(4)=0 asn(4) tlvlist`. **Checksum = plain Internet ones-complement checksum over the ENTIRE EIGRP message (header with chksum field zeroed, + all TLVs)** — confirmed via `post_build`'s `checksum(p)` call on the full built packet, not scoped to a sub-range. `EIGRPParam` TLV: read `scapy-eigrp.py` for its type code and k1/k3/holdtime field layout.
- ospf_rogue: `Ether(src=IfaceMAC,dst=01:00:5e:00:00:05)/BuildIPv4(src=router_id,dst=224.0.0.5,proto=89,payload=ospfMsg)`. OSPF_Hdr (24 bytes, per `trim-ospf.py`): `version(1)=2 type(1)=1 len(2,auto=24+OSPF_Hello-length) src(4)=router_id area(4)=area chksum(2) authtype(2)=0 authdata(8)=0` then OSPF_Hello body. **Checksum = Internet checksum over `header[0:16] + payload[24:]`** — i.e. EXCLUDES the 8-byte authdata field at offset 16-23 (confirmed via `post_build`'s `checksum(p[:16] + p[24:])`; this is NOT the same rule as EIGRP's full-packet checksum, do not copy-paste one into the other). OSPF_Hello fields: `mask, hellointerval, options, prio, deadinterval, router, backup` per the script's kwargs.
- hsrp_hijack: `Ether(src=vmac,dst=01:00:5e:00:00:02)/BuildIPv4(src=InterfaceIPv4(eth1),dst=224.0.0.2,proto=17,payload=BuildUDP(sport=1985,dport=1985,payload=hsrpMsg))`. **HSRP has NO protocol checksum field at all** (corrected from rev 1's vague "own algorithm" claim — confirmed in `scapy-hsrp.py`: no chksum in `fields_desc`). Integrity is only the UDP checksum from `BuildUDP`. HSRP body (20 bytes, per `scapy-hsrp.py`): `version(1) opcode(1) state(1) hellotime(1)=3 holdtime(1)=10 priority(1) group(1) reserved(1)=0 auth(8)="cisco\0\0\0" virtualIP(4)`.
- vrrp_hijack: `Ether(src=vmac,dst=01:00:5e:00:00:12)/BuildIPv4(src=InterfaceIPv4(eth1),dst=224.0.0.18,proto=112,payload=vrrpMsg)`. VRRP (v2) body per `scapy-vrrp.py`: `version(4bit)=2 | type(4bit)=1` packed into 1 byte (`0x21`), `vrid(1) priority(1) ipcount(1,auto=len(addrlist)=1) authtype(1)=0 adv(1)=1 chksum(2) addrlist(4*ipcount) auth1(4)=0 auth2(4)=0`. **Checksum = plain Internet checksum over the WHOLE VRRP body (chksum field zeroed), with NO IP pseudo-header** (VRRPv2-specific — confirmed via `post_build`'s bare `checksum(p)`; this differs from VRRPv3, which the Python does not use, and differs from UDP/TCP-style pseudo-header checksums — do not add one).

**Batch 3 — send+receive, IPv4, 3 modules:** arp_scan, dhcp_discover, dhcp_rogue.
Use Batch 0's `RawReceiver`/parsers/`BuildDHCPDiscover`/`BuildDHCPOffer`
directly — no attackcommon edits.
- arp_scan: open the `RawReceiver` **before** sending (same race-avoidance the Python's AsyncSniffer-before-send ordering achieves), `HostsInCIDR(--subnet)`, send one ARP request per host (`hwsrc=IfaceMAC, psrc=InterfaceIPv4, pdst=each host`), collect ARP replies (op=2) for 3s, log `[HOST] ip=%s mac=%s` — this exact line format IS parsed by the GUI (`gui/util.go parseReconHosts` has a live regex for it, confirmed) and must match byte-for-byte.
- dhcp_discover: open the receiver before sending (same race note the Python comment makes explicit), send `BuildDHCPDiscover`, parse UDP 67↔68 + BOOTP for `--duration` seconds, log `[SERVER] dhcp-server=%s offered=%s` per unique server. (Note: unlike `[HOST]`, there is currently no GUI-side regex parser for `[SERVER]` — it only affects the ring-buffer log view's coloring, not a structured feature. Still match the text exactly since a future GUI feature or test could start depending on it, and it costs nothing extra.)
- dhcp_rogue: receive-loop for `--interval` (or 5s if 0) seconds, parse BOOTP DISCOVER (option 53==1), extract xid+chaddr **verbatim from the received frame** (including any trailing zero padding — `chaddr.hex()` in the Python's log line is the full 16-byte padded value, not just the 6 real MAC bytes; log the same 32 hex characters), build+send `BuildDHCPOffer`, cycling `next_ip()` through `--pool_start`..`--pool_end` exactly as the Python does (wrap to `pool_start` when `cursor >= pool_end`).

**Batch 4 — IPv6 + passive-only, 2 modules:** sniff, ra_spoof.
- sniff: pure receive (no `RawSender` needed at all). `--selftest` mirrors the Python's trivial no-op selftest (it doesn't build a real packet either — just report success, optionally referencing the Ethernet/Dot1Q types to mirror the spirit of the Python's `_ = Ether()/Dot1Q(vlan=10)`). Use `ParseEthernet`'s VLAN-tag return value for the `Dot1Q.vlan` tally. Tie-break sort order for the top-10 talkers: Python's `sorted(macs.items(), key=lambda kv: kv[1], reverse=True)` is a **stable** sort — ties keep first-seen (insertion) order, since Python's sort is stable and dict iteration order is insertion order (3.7+). Go's `sort.Slice` is NOT guaranteed stable — use `sort.SliceStable` and build the candidate slice in first-seen order to match. VLAN summary line: Python prints `sorted(vlans)` (a Python list repr, e.g. `[10, 20]`) or the literal string `"(untagged only)"` if empty — match that exact formatting, not Go's default slice-print format.
- ra_spoof: `Ether(dst=33:33:00:00:00:01,src=IfaceMAC)/IPv6(dst=ff02::1,src=<eth1 link-local>,nh=58,hlim=255)/ICMPv6ND_RA(...)/PrefixInfo/SrcLLAddr/RDNSS` per `trim-inet6.py`'s exact bitfields (note the RA's bit-packed byte 4: `chlim(8) | M(1) O(1) H(1) prf(2) P(1) res(2)`, default M=O=H=P=res=0, prf=1 per scapy's default — the Python doesn't override these, so they carry scapy's non-zero `prf` default, not all-zero). Checksum via the RFC 2460 §8.1 pseudo-header algorithm in Batch 0's `BuildUDP`-adjacent helper (reuse a shared `IPv6PseudoheaderChecksum` if it's cleaner to add one — this is a read from Batch 0's file, adding a new exported func here is fine since Batch 0 is already merged and frozen by the time this batch runs, just don't edit existing Batch 0 functions). Best-effort DHCPv6: receive-loop briefly (`min(2.0, max(0.1,interval))`) for a DHCP6_Solicit (UDP 546→547), and if seen, reply via UDP 547→546 with a DHCP6_Advertise: `msgtype(1)=2 trid(3, copied from the Solicit) + [ClientID option copied verbatim if present, else omitted] + ServerID option (optcode=2, duid=the exact 10 raw bytes `0003000102000000aabb` from the Python — do not construct/decode this as a DUID_LLT object, it's an opaque blob in the Python too) + DNSServers option (optcode=23, dns list=[--dns_server])`. Option order: **ClientID first (if present), then ServerID, then DNSServers** — confirmed from the Python's `reply = DHCP6_Advertise(...) / cid / DHCP6OptServerId(...) / DHCP6OptDNSServers(...)` construction order when `cid` exists. Wrap this whole half in the same best-effort try/log-WARN pattern the Python uses (it's explicitly optional).

## Integration gate (orchestrator, after all batches land)

1. `git status --short` — confirm scope: only `tools/secbench-attacks-go/**` touched (plus the two `attackcommon.go` log-format lines already fixed).
2. `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./...`, `go vet ./...`, `go test ./...`.
3. Deploy all 18 binaries to the VM, rename a veth to `eth1` (spike technique), run every module's `--selftest`.
4. **Byte-diff every outbound-only module** (not a sample — rev 1's "spot check a few" is replaced): mac_flood, mac_spoof, stp_root, vlan_hop, dtp_hop, lldp_flood, eigrp_rogue, ospf_rogue, hsrp_hijack, vrrp_hijack, dhcp_starve, ra_spoof's RA frame — against the same Python script, live capture, byte-diff. Normalize (mask) ONLY: genuinely-random forged MACs (mac_flood/mac_spoof-if-random/lldp/dtp/stp's per-run random MAC) and dhcp_starve's random XID+chaddr **and every checksum byte that depends on a masked field** (e.g. dhcp_starve's UDP checksum changes when chaddr/xid change — mask that too, don't let it produce a false failure or tempt masking the whole frame). For the HSRP/VRRP/mac_spoof/vlan_hop deterministic-source-IP rows in the inventory table above, the Go and Python frames will legitimately differ in IP src (and dependent IP/UDP/protocol checksums) — this is the documented, intentional deviation, not a bug: verify it by confirming the Go IP.src equals the VM's actual eth1 IPv4, not by trying to force byte parity.
5. **Functional (not byte-diff) live test** for arp_scan, dhcp_discover, dhcp_rogue, sniff, and ra_spoof's DHCPv6 half — these need an actual peer to reply to/observe. Two-node lab topology, verify the expected log lines appear with correct data.
6. Report a clear per-module verdict table to the user, explicitly calling out the deterministic-source-IP deviation (mac_spoof, vlan_hop, hsrp_hijack, vrrp_hijack, ra_spoof) as a reasoned improvement, not silently.
