#!/usr/bin/env python3
"""RECON: passively observe eth1 and summarize MACs, VLAN tags and the
top talkers seen — no packets are ever sent by this module."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def main():
    p = base_parser("Passively sniff eth1 and summarize what's on the wire")
    p.add_argument("--duration", type=int, default=20, help="seconds to sniff per round")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    if args.selftest:
        # Nothing is built to send — sniff is receive-only. Prove the layers
        # this module inspects are importable/usable instead.
        _ = scapy_all.Ether() / scapy_all.Dot1Q(vlan=10)
        selftest_ok("sniff", "Ether/Dot1Q layers available, no packets sent")
        return

    def one_round(n):
        status("INFO", "round #%d: sniffing %s for %ds" % (n + 1, args.iface, args.duration))
        macs = {}
        vlans = set()

        def on_pkt(pkt):
            if pkt.haslayer(scapy_all.Ether):
                src = pkt[scapy_all.Ether].src
                macs[src] = macs.get(src, 0) + 1
            if pkt.haslayer(scapy_all.Dot1Q):
                vlans.add(pkt[scapy_all.Dot1Q].vlan)

        scapy_all.sniff(iface=args.iface, timeout=args.duration, prn=on_pkt, store=0)
        top = sorted(macs.items(), key=lambda kv: kv[1], reverse=True)[:10]
        for mac, cnt in top:
            status("TALKER", "mac=%s frames=%d" % (mac, cnt))
        status("INFO", "round #%d: %d unique MAC(s), VLANs seen: %s" %
               (n + 1, len(macs), sorted(vlans) if vlans else "(untagged only)"))

    run_loop(args.count, args.interval, one_round)


if __name__ == "__main__":
    main()

