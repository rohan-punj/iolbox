# Security Bench tool pack

This pack contains the 18 L2/L3 Scapy learning modules that operate against Cisco IOL/VPCS lab segments. The standalone GUI listens on the per-node IOLBOX_TOOL_SOCK AF_UNIX socket and reads IOLBOX_TOOL_OPTIONS.

The GUI's compiled module definitions and this manifest are checked one-to-one by moduledefs_test.go. The pack intentionally has no NGFW/firewall modules, no fw_reach helper, and no Victim Mode target.

The pack ships 18 static Go binaries under `bin/`, built from `tools/secbench-attacks-go/`. They have no Python, venv, wheelhouse, or runtime package dependencies.
