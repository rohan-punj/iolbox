#!/usr/bin/env python3
"""Shared argument parsing + guard rails for pnet-secbench attack/recon helpers.

Every script in this directory is a small, standalone process spawned by the
Go GUI supervisor (see gui/runner.go). They share this module so behavior is
identical across all ~18 of them:

  * --iface is locked to the lab NIC (eth1). The Go GUI never lets a user
    choose an interface and always hardcodes --iface eth1 (runner.go), but
    every helper independently refuses eth0/docker0/lo so a manual/direct
    invocation cannot bypass the safety rule either (defense in depth).
  * --selftest builds the packet(s) in memory and prints OK WITHOUT sending
    anything — this is what smoke.sh drives so the gate can prove a helper
    constructs valid packets without flooding the gate network.
  * status() prints one structured line per event; the Go ring buffer
    captures stdout verbatim and the GUI colours lines by their [LEVEL] tag.
  * run_loop() is the shared send/repeat skeleton: --count 0 means "until
    the Go supervisor kills this process", --count N sends N times.
"""
import argparse
import sys
import time

ALLOWED_IFACE = "eth1"  # the ONLY interface any attack/recon helper may bind to


def status(level, msg):
    """Structured stdout line: the Go ring buffer + GUI log view read these."""
    print("[%s] %s %s" % (level, time.strftime("%H:%M:%S"), msg), flush=True)


def base_parser(description):
    p = argparse.ArgumentParser(description=description)
    p.add_argument("--iface", default="eth1",
                    help="lab NIC to operate on — MUST be eth1, never eth0/docker0/lo")
    p.add_argument("--selftest", action="store_true",
                    help="build the packet(s) in memory and print OK, do not send anything")
    p.add_argument("--count", type=int, default=0,
                    help="number of iterations; 0 = run until stopped")
    p.add_argument("--interval", type=float, default=1.0,
                    help="seconds to sleep between iterations")
    return p


def enforce_lab_iface(iface):
    """Hard safety rail: refuse to run on anything but the lab NIC.

    This is defense-in-depth. The primary enforcement point is the Go GUI
    supervisor (gui/runner.go Supervisor.Start), which hardcodes --iface
    eth1, never exposes an iface field on any module's form, and strips any
    --iface/-i token a user could otherwise smuggle in through the Raw-args
    field (stripIfaceFlag). This check exists so a helper invoked directly
    (smoke.sh's explicit "refuses eth0" test, a shell on the box, or any
    future caller) is ALSO refused, not just the GUI-driven path.

    ALLOWLIST, not a denylist: anything other than eth1 is fatal. An earlier
    version only denied eth0/docker0/lo and merely warned on anything else,
    which let a bridge or other unexpected interface name through uncaught.
    """
    if iface != ALLOWED_IFACE:
        status("FATAL", "refusing to run on %r — attacks are locked to the lab NIC (%s) only" % (iface, ALLOWED_IFACE))
        sys.exit(2)


def require_scapy():
    try:
        import scapy.all  # noqa: F401
        return scapy.all
    except ImportError as e:
        status("FATAL", "scapy is not importable in this venv: %s" % e)
        sys.exit(3)


def forged_mac():
    """A random locally-administered *unicast* MAC (02:xx:...) for use as the L2
    source of the forged-identity floods (LLDP/CDP).

    Frames MUST carry a valid, non-zero source MAC. PNetLab links are Linux
    bridges, and br_handle_frame() drops any frame whose source fails
    is_valid_ether_addr() (all-zero or multicast) BEFORE forwarding it to the
    peer node. scapy's sendp() does not auto-fill the source on a bare
    Ether()/Dot3() in this container (conf.iface is not the send iface), so an
    unset source serialises to 00:00:00:00:00:00 and the flood is silently
    dropped at the bridge instead of reaching the neighbour. Forging a fresh
    random source per frame fixes that and is more realistic — each forged
    announcement looks like a distinct neighbour.
    """
    import random
    return "02:%02x:%02x:%02x:%02x:%02x" % tuple(
        random.randint(0, 255) for _ in range(5))


def iface_mac(iface):
    """The lab NIC's own hardware MAC, for attacks whose forged frames must look
    like they originate from the attacker itself so replies come back to us
    (ARP scan/poison, DHCPDISCOVER/OFFER, VLAN-hop injection).

    Same bridge-drop rationale as forged_mac(): a frame with an all-zero L2
    source is dropped by the PNetLab link bridge (br_handle_frame() ->
    is_valid_ether_addr) BEFORE it reaches the neighbour. scapy's sendp() does
    not auto-fill the Ether source on a bare Ether()/Dot3() here (conf.iface is
    not the send iface), and for a broadcast/ARP destination the SourceMACField
    route lookup yields 00:00:00:00:00:00 — so we set it explicitly to the lab
    NIC's real MAC. Unlike the forged-identity floods (LLDP/CDP/DHCP-starve),
    these attacks want a *stable, real* source so the victim's reply is unicast
    back to the attacker.

    Falls back to a random locally-administered MAC (forged_mac) if the iface
    can't be read — e.g. a --selftest packet-build in a bare container with no
    eth1 — so packet construction never depends on the NIC actually existing.
    """
    try:
        from scapy.all import get_if_hwaddr
        mac = get_if_hwaddr(iface)
        if mac and mac != "00:00:00:00:00:00":
            return mac
    except Exception:
        pass
    return forged_mac()


def iface_ipv4(iface):
    """The lab NIC's own IPv4 address, or "0.0.0.0" if it has none.

    Used as the ARP sender protocol address (psrc) so a target can unicast its
    ARP reply back to us. If eth1 is unnumbered we fall back to 0.0.0.0 — the
    reply still can't be routed by IP then, but the sender *hardware* address
    (iface_mac) is what actually matters for the L2 return path.
    """
    try:
        from scapy.all import get_if_addr
        ip = get_if_addr(iface)
        if ip and ip != "0.0.0.0":
            return ip
    except Exception:
        pass
    return "0.0.0.0"


def run_loop(count, interval, send_fn):
    """Call send_fn(n) for n in 0..count-1 (or forever if count<=0), sleeping
    `interval` seconds between calls. Handles Ctrl-C / SIGTERM (the Go
    supervisor kills the process outright, but this keeps direct runs tidy).
    """
    n = 0
    try:
        while count <= 0 or n < count:
            send_fn(n)
            n += 1
            if count <= 0 or n < count:
                time.sleep(max(0.0, interval))
    except KeyboardInterrupt:
        pass
    status("INFO", "stopped after %d iteration(s)" % n)


def selftest_ok(name, detail=""):
    status("OK", "selftest %s: packet builds cleanly%s" % (name, (" — " + detail) if detail else ""))
    print("PASS: selftest %s" % name, flush=True)


# ---------------------------------------------------------------------------
# NGFW test helpers
# ---------------------------------------------------------------------------
# The "NGFW Tests" module group fires application-layer traffic (HTTP/HTTPS,
# DNS) AT a next-gen firewall toward a server behind it, so a student can watch
# the matching security profile block it. These flows go out through the OS
# socket layer, not scapy, so the eth1 lock is enforced a different way: every
# outbound socket is pinned to the lab NIC with SO_BINDTODEVICE(eth1). The
# kernel then egresses that traffic out eth1 regardless of the routing table,
# exactly like the scapy helpers' sendp(iface=eth1). Requires CAP_NET_RAW (the
# node already runs with --cap-add=NET_ADMIN --cap-add=NET_RAW).
import socket as _socket

_SO_BINDTODEVICE = getattr(_socket, "SO_BINDTODEVICE", 25)


def _dev_socket(host, port, timeout, dev):
    """A connected TCP socket hard-bound to `dev` (eth1) before connect()."""
    s = _socket.socket(_socket.AF_INET, _socket.SOCK_STREAM)
    s.setsockopt(_socket.SOL_SOCKET, _SO_BINDTODEVICE, dev.encode() + b"\0")
    if timeout is not None:
        s.settimeout(timeout)
    s.connect((host, port))
    return s


def iface_opener(iface, timeout=8.0):
    """A urllib opener whose every HTTP/HTTPS connection is pinned to `iface`.

    Lab servers routinely use self-signed certs, so TLS verification is off —
    we are testing whether the firewall inspects/blocks the flow, not the
    server's PKI.
    """
    import http.client
    import ssl
    import urllib.request

    dev = iface
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE

    class _HTTPConn(http.client.HTTPConnection):
        def connect(self):
            self.sock = _dev_socket(self.host, self.port, self.timeout, dev)

    class _HTTPSConn(http.client.HTTPSConnection):
        def connect(self):
            raw = _dev_socket(self.host, self.port, self.timeout, dev)
            self.sock = ctx.wrap_socket(raw, server_hostname=self.host)

    class _HTTPHandler(urllib.request.HTTPHandler):
        def http_open(self, req):
            return self.do_open(_HTTPConn, req)

    class _HTTPSHandler(urllib.request.HTTPSHandler):
        def https_open(self, req):
            return self.do_open(_HTTPSConn, req)

    return urllib.request.build_opener(_HTTPHandler, _HTTPSHandler)


def http_probe_report(iface, url, method="GET", data=None, headers=None,
                      label="request", timeout=8.0):
    """Fire one HTTP(S) request out eth1 and narrate the firewall's verdict.

    A completed response means the firewall ALLOWED/passed the flow; a reset,
    timeout or refused connection means a security profile most likely fired
    (dropped/reset the session). For AV/IPS/URL tests the block is the teaching
    win — the log line says so explicitly.
    """
    import urllib.error
    opener = iface_opener(iface, timeout=timeout)
    req = urllib.request.Request(url, data=data, method=method, headers=headers or {})
    status("SENT", "%s: %s %s" % (label, method, url))
    try:
        resp = opener.open(req, timeout=timeout)
        body = resp.read(256)
        status("OK", "%s -> HTTP %s %s (%d bytes read) — firewall ALLOWED this flow"
               % (label, resp.status, resp.reason, len(body)))
        return resp.status
    except urllib.error.HTTPError as e:
        status("WARN", "%s -> HTTP %s %s — server answered; firewall passed the session"
               % (label, e.code, e.reason))
        return e.code
    except Exception as e:
        status("BAD", "%s -> blocked/failed: %s (%s) — likely the firewall profile "
               "fired (reset/drop). Check the firewall's Threat/URL/Data log."
               % (label, type(e).__name__, e))
        return None


# The industry-standard EICAR anti-malware *test* string (not malware — a
# harmless 68-byte marker every AV engine flags on purpose). Split so this
# source file itself is not flagged by a scanner watching the repo.
def eicar_bytes():
    return (b"X5O!P%@AP[4\\PZX54(P^)7CC)7}" +
            b"$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!" + b"$H+H*")

