#!/usr/bin/env python3
"""RECON: ARP-scan a subnet for live hosts.

Prints one "[HOST] ip=<ip> mac=<mac>" line per responder — the Go GUI parses
these (see gui/util.go parseReconHosts) to prefill target fields elsewhere.
"""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, selftest_ok, iface_mac, iface_ipv4  # noqa: E402


def main():
    p = base_parser("ARP-scan a subnet for live hosts")
    p.add_argument("--subnet", default="", help="CIDR to scan, e.g. 192.168.1.0/24")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    from scapy.all import ARP, Ether, srp

    # A valid (non-zero) L2 source is required or the PNetLab link bridge drops
    # the request before forwarding it — see common.iface_mac(). We ALSO set the
    # ARP sender fields explicitly: in this container scapy's auto-resolution of
    # ARP.hwsrc/psrc fails (route lookup for a broadcast target), leaving hwsrc
    # 00:00:00:00:00:00 and psrc 0.0.0.0, so the target has no address to unicast
    # its reply to and srp() never matches an answer. hwsrc = the lab NIC's real
    # MAC (the L2 return path); psrc = the lab NIC's IPv4 (or 0.0.0.0 if none).
    src = iface_mac(args.iface)
    psrc = iface_ipv4(args.iface)
    pkt = Ether(src=src, dst="ff:ff:ff:ff:ff:ff") / ARP(hwsrc=src, psrc=psrc, pdst="192.168.1.1")
    if args.selftest:
        selftest_ok("arp_scan", "ARP request len=%d hwsrc=%s psrc=%s" % (len(bytes(pkt)), src, psrc))
        return

    if not args.subnet:
        status("FATAL", "no --subnet given")
        sys.exit(1)

    def sweep(n):
        status("INFO", "ARP sweep #%d of %s on %s (hwsrc=%s psrc=%s)" % (n + 1, args.subnet, args.iface, src, psrc))
        ans, _ = srp(Ether(src=src, dst="ff:ff:ff:ff:ff:ff") / ARP(hwsrc=src, psrc=psrc, pdst=args.subnet),
                      timeout=3, iface=args.iface, verbose=0)
        for _, rcv in ans:
            status("HOST", "ip=%s mac=%s" % (rcv[ARP].psrc, rcv[ARP].hwsrc))
        status("INFO", "sweep #%d complete: %d host(s)" % (n + 1, len(ans)))

    from common import run_loop
    run_loop(args.count, args.interval, sweep)


if __name__ == "__main__":
    main()

