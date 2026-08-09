#!/usr/bin/env python3
"""RECON: broadcast DHCPDISCOVER and report every server that answers — the
simplest way to find rogue DHCP servers on a segment (compare against the
one the network team expects)."""
import os
import random
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, iface_mac  # noqa: E402


def rand_mac_bytes():
    b = [0x02, 0x00, 0x00] + [random.randint(0, 255) for _ in range(3)]
    return bytes(b)


def build_discover(scapy_all, xid, src):
    Ether, IP, UDP, BOOTP, DHCP = (scapy_all.Ether, scapy_all.IP, scapy_all.UDP,
                                    scapy_all.BOOTP, scapy_all.DHCP)
    chaddr = rand_mac_bytes()
    # A valid (non-zero) L2 source is required or the PNetLab link bridge drops
    # the broadcast before any server sees it — see common.iface_mac(). Use the
    # lab NIC's real MAC (this recon wants the OFFERs to come back to us).
    return (Ether(src=src, dst="ff:ff:ff:ff:ff:ff") /
            IP(src="0.0.0.0", dst="255.255.255.255") /
            UDP(sport=68, dport=67) /
            BOOTP(chaddr=chaddr, xid=xid, flags=0x8000) /
            DHCP(options=[("message-type", "discover"), "end"]))


def main():
    p = base_parser("Broadcast DHCPDISCOVER and report responding servers")
    p.add_argument("--duration", type=int, default=10, help="seconds to listen for OFFERs")
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    src = iface_mac(args.iface)
    if args.selftest:
        pkt = build_discover(scapy_all, xid=1234, src=src)
        selftest_ok("dhcp_discover", "DHCPDISCOVER len=%d" % len(bytes(pkt)))
        return

    def one_round(n):
        xid = random.randint(1, 0xFFFFFFFF)
        seen = set()

        def on_pkt(pkt):
            if pkt.haslayer(scapy_all.DHCP):
                opts = dict((o[0], o[1]) for o in pkt[scapy_all.DHCP].options if isinstance(o, tuple))
                if opts.get("message-type") == 2:  # DHCPOFFER
                    server = pkt[scapy_all.IP].src if pkt.haslayer(scapy_all.IP) else "?"
                    offered = pkt[scapy_all.BOOTP].yiaddr if pkt.haslayer(scapy_all.BOOTP) else "?"
                    if server not in seen:
                        seen.add(server)
                        status("SERVER", "dhcp-server=%s offered=%s" % (server, offered))

        # Start listening BEFORE sending: a low-latency rogue server (the exact
        # adversary this recon finds) can answer in a few ms — sending first and
        # then binding the sniffer races the OFFER and silently misses it.
        sniffer = scapy_all.AsyncSniffer(iface=args.iface,
                                         filter="udp and (port 67 or port 68)",
                                         prn=on_pkt, store=0)
        sniffer.start()
        status("INFO", "round #%d: broadcasting DHCPDISCOVER (xid=%d) on %s" % (n + 1, xid, args.iface))
        scapy_all.sendp(build_discover(scapy_all, xid, src), iface=args.iface, verbose=0)
        try:
            sniffer.join(timeout=args.duration)
        finally:
            try:
                sniffer.stop()
            except Exception:
                pass
        status("INFO", "round #%d complete: %d server(s) answered" % (n + 1, len(seen)))

    run_loop(args.count, args.interval, one_round)


if __name__ == "__main__":
    main()

