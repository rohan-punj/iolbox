iolab native server install
============================

This tarball installs iolab (the Go supervisor that runs Cisco IOL + VPCS
lab nodes, with an embedded browser GUI) directly onto any systemd-based
x86-64 glibc Linux server: bare metal, a cloud VM, an on-prem hypervisor
guest, whatever you've got. It is the "bring your own Linux box" install
target — see docs/providers.md's `remote` provider in the iolab repo for
how the Windows-side app talks to a host set up this way.

Requirements
------------
- systemd
- x86-64 (amd64), glibc-based distro (Debian/Ubuntu, Fedora/RHEL, etc.)
- root access
- iproute2 (`ip`), procps (`sysctl`), iptables, sudo — installed
  automatically via apt-get if present; otherwise install.sh lists them
  and exits so you can install them with your distro's package manager
- optional: your distro's OpenSSL 3.x runtime library package (some IOL
  images dlopen libcrypto) — Debian/Ubuntu: libssl3, Fedora/RHEL: openssl-libs

Install
-------
    tar xzf iolab-server-<version>.tar.gz
    cd iolab-server-<version>
    sudo ./install.sh                  # binds GUI/console/capture to 127.0.0.1
    sudo ./install.sh --bind all       # binds 0.0.0.0 (LAN/VPN/tunnel reachable)

install.sh installs to /opt/iolab, sets up the systemd units, generates the
IOL license (iourc) immediately, and starts the supervisor. It prints the
GUI URL when done.

SECURITY: the GUI/WS bridge (:4001), native telnet consoles (:9000+), and
native Wireshark capture tees (:5500+) have NO AUTHENTICATION. --bind all
makes them reachable from anything that can route to this host. Use on a
trusted LAN/VPN, or behind an SSH tunnel with --bind local (the default).
Never expose these ports on a host with a public IP.

Hostname / IOL license
-----------------------
The IOL license (iourc) is generated from this machine's own hostid AND
hostname, and IOL checks it against the RUNNING hostname on every node
start. Keep the hostname stable after install — do NOT rename the host,
and if this is a cloud instance, make sure cloud-init isn't set to
randomize the hostname on every boot (install.sh warns if it looks like
that might be the case).

If the license ever breaks (nodes refuse to start with a license error):
    sudo rm -f /opt/iolab/.iourc-generated /opt/iolab/iourc
    sudo systemctl restart iolab-firstboot-iourc.service
    sudo systemctl restart iolab-supervisor.service

Changing the bind mode later
-----------------------------
    sudo $EDITOR /etc/iolab/bind.env      # edit IOLAB_WS_ADDR / CONSOLE / CAPTURE
    sudo systemctl restart iolab-supervisor.service

Uninstall
---------
    sudo ./uninstall.sh              # prompts before deleting images/labs
    sudo ./uninstall.sh --yes        # non-interactive, deletes everything

Logs and status
----------------
    journalctl -u iolab-supervisor -f
    systemctl status iolab-supervisor iolab-firstboot-iourc

Tarball contents
-----------------
    install.sh                              installer (see above)
    uninstall.sh                            uninstaller
    README.txt                              this file
    bin/supervisor                          the Go supervisor binary (linux/amd64)
    bin/vpcs                                VPCS binary (linux/amd64, static)
    opt-iolab/firstboot-iourc.sh             first-boot IOL license generator
    opt-iolab/prestart-clean.sh              ExecStartPre stale-state sweep
    systemd/iolab-supervisor.service         main unit (unmodified stock unit)
    systemd/iolab-firstboot-iourc.service    firstboot oneshot unit
    systemd/10-bind.conf                     drop-in: bind addrs from an EnvironmentFile
    systemd/bind.env.local                   --bind local env file (127.0.0.1 everywhere)
    systemd/bind.env.all                     --bind all env file (0.0.0.0 everywhere)
    etc/99-iolab.conf                        sysctl drop-in (ip_forward off by default)
