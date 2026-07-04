# iolbox Docker image — Linux runtime for IOL/VPCS, packaged as a container
# instead of the WSL2/VMware appliance. Same rootfs contract as
# runtime/build-rootfs.sh (see runtime/README.md): one supervisor binary,
# one vpcs binary, the same package set, the same /opt/iolbox layout. A bug
# fixed in the appliance should be fixed here too, and vice versa.
#
# BUILD CONTEXT: repo root (not docker/), because stage 2 COPYs the
# prebuilt supervisor binary from ../supervisor/bin/. Build it first:
#
#   bash build-release.sh
#   docker build -f docker/Dockerfile -t iolbox:latest .
#
# (docker-compose.yml sets `context: ..` / `dockerfile: docker/Dockerfile`
# for you — see docker/compose.yml and docker/README.md.)

# ---------------------------------------------------------------------------
# Stage 1: build vpcs from source (mirrors runtime/fetch-vpcs.sh exactly —
# same fork, same pinned tag, same static link, same Makefile.linux path).
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim AS vpcs-builder

# git/make/gcc + libc6-dev for the static glibc link (see fetch-vpcs.sh's
# comment on why vpcs is linked statically: this builder's glibc must not
# be newer than the final image's, so building INSIDE the same
# debian:bookworm-slim base as stage 2 sidesteps that whole problem).
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        git \
        make \
        gcc \
        libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Same repo + tag as runtime/fetch-vpcs.sh's VPCS_REPO/VPCS_REF defaults.
# Bump deliberately in both places together if this ever moves.
ARG VPCS_REPO=https://github.com/GNS3/vpcs.git
ARG VPCS_REF=v0.8.3

RUN git clone --branch "${VPCS_REF}" --depth 1 "${VPCS_REPO}" /usr/src/vpcs \
    || (rm -rf /usr/src/vpcs && git clone "${VPCS_REPO}" /usr/src/vpcs && git -C /usr/src/vpcs checkout "${VPCS_REF}")

WORKDIR /usr/src/vpcs/src
# Same LDFLAGS as fetch-vpcs.sh: static link, -lpthread -lutil.
RUN make -f Makefile.linux LDFLAGS="-static -lpthread -lutil" \
    && mkdir -p /out \
    && install -m 0755 vpcs /out/vpcs

# ---------------------------------------------------------------------------
# Stage 2: runtime image — debian:bookworm-slim + the same package set as
# runtime/build-rootfs.sh's BASE_INCLUDE (minus systemd/udev/openssh-client,
# which are appliance-only concerns — see the package-diff notes below) +
# i386 multiarch (32-bit IOL support, matches build-rootfs.sh's default
# INCLUDE_I386=1) + supervisor + vpcs + entrypoint.
# ---------------------------------------------------------------------------
FROM debian:bookworm-slim AS runtime

# Package set mirrored from runtime/build-rootfs.sh's BASE_INCLUDE, with the
# following DELIBERATE diffs (documented, not silent):
#   - systemd, systemd-sysv, udev: DROPPED. This image has no init system —
#     entrypoint.sh IS pid 1 (see that file). Nothing here needs udev device
#     events; tap/macvtap creation goes through `ip` directly, same as the
#     appliance's own runtime path (systemd is a boot-time concern, not a
#     supervisor-runtime one).
#   - openssh-client: DROPPED. build-rootfs.sh keeps it only "as a handy
#     debugging tool for the runtime maintainers (scp'ing a log out, etc.)"
#     per its own comment — not load-bearing. `docker cp`/`docker exec`
#     cover the same debugging need for a container, so it's not carried.
#   - iproute2, iputils-ping, libssl3, sudo, procps, iptables: KEPT, same
#     reasons as build-rootfs.sh's comment block (sudo: extnet always shells
#     `sudo -n ip/iptables/sysctl`, even as root, and detect_linux.go probes
#     `sudo -n true` — the BINARY must exist; procps: /usr/sbin/sysctl;
#     iptables: NAT node MASQUERADE/FORWARD; libssl3: some IOL images dlopen
#     libcrypto).
#   - coreutils' `hostid`: already present in bookworm-slim's base (no extra
#     package needed) — used by -gen-iourc, see the hostid/iourc note in
#     docker/README.md.
#   - ca-certificates: still omitted, same reasoning as build-rootfs.sh (no
#     outbound TLS from the runtime itself).
RUN apt-get update && apt-get install -y --no-install-recommends \
        iproute2 \
        iputils-ping \
        libssl3 \
        sudo \
        procps \
        iptables \
    && dpkg --add-architecture i386 \
    && apt-get update && apt-get install -y --no-install-recommends \
        libc6:i386 \
        libssl3:i386 \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/*

# /opt/iolbox layout — identical to build-rootfs.sh Stage 4.
RUN install -d -m 0755 /opt/iolbox/images \
    && install -d -m 0755 /opt/iolbox/run \
    && install -d -m 0755 /opt/iolbox/labs

# Supervisor binary — built by build-release.sh at the repo root BEFORE
# `docker build` runs (this Dockerfile does not build Go/Node itself; see
# the header comment and docker/README.md). Context is the repo root, so
# this path is relative to that.
COPY supervisor/bin/supervisor-linux-amd64 /opt/iolbox/supervisor

# vpcs, built in stage 1.
COPY --from=vpcs-builder /out/vpcs /opt/iolbox/vpcs

# entrypoint + prestart cleanup + firstboot-iourc-equivalent logic (all one
# script here — see entrypoint.sh's header for why this isn't split into
# separate systemd-style units the way the appliance is).
COPY docker/entrypoint.sh /opt/iolbox/entrypoint.sh

RUN chmod 0755 /opt/iolbox/supervisor /opt/iolbox/vpcs /opt/iolbox/entrypoint.sh

# /opt/iolbox on PATH: the supervisor spawns `vpcs` by bare name (see
# supervisor/internal/node/argv.go), matching iolbox-supervisor.service's
# Environment=PATH= line exactly.
ENV PATH="/opt/iolbox:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

WORKDIR /opt/iolbox

# 4001 = ws-addr (GUI + WS control/console bridge). Console (9000-9999) and
# capture (5500-6499) ranges are published via compose.yml, not baked in as
# EXPOSE ranges here (EXPOSE is documentation-only for `docker run`; the
# real port math lives in compose.yml where it's verified against
# supervisor/internal/server/server.go — see docker/README.md).
EXPOSE 4001

ENTRYPOINT ["/opt/iolbox/entrypoint.sh"]
