#!/bin/sh
# Generate dropbear's host keys on first boot if they don't exist yet.
# dropbear-bin ships no init glue to do this (unlike Debian's dropbear
# package), so iolbox-dropbear.service's ExecStartPre runs this directly.
set -e
mkdir -p /etc/dropbear
[ -f /etc/dropbear/dropbear_rsa_host_key ] || dropbearkey -t rsa -f /etc/dropbear/dropbear_rsa_host_key
[ -f /etc/dropbear/dropbear_ecdsa_host_key ] || dropbearkey -t ecdsa -f /etc/dropbear/dropbear_ecdsa_host_key
