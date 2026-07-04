#!/usr/bin/env python3
"""
import-pack.py — convert an existing PNetLab/EVE-NG IOL lab (.unl) into an
iolbox lab JSON document (contracts/lab.schema.json, version 1).

Scope: IOL nodes and VPCS only (iolbox's supported kinds). Other node types are
skipped with a warning. This gives an instant content library from the existing
CCNA/CCNP/CCIE IOL packs.

Usage:
    python import-pack.py path/to/lab.unl [-o out.lab.json]

Notes / mapping:
- .unl is XML (<lab><topology><nodes><node .../></nodes><networks/> ...).
- IOL node.type == "iol"; image is the node's "image" attr (kept as filename
  fallback; the iolbox image library resolves the real content id on import).
- Interface addressing: EVE/PNet IOL uses Ethernet/Serial adapter+port; we emit
  iolbox interface strings like "e0/0"/"s1/2". NETMAP id = node.id in both models.
- Links: EVE models a link as a <network> that node <interface>s reference by
  network_id. Two interfaces on one network => p2p; >2 => segment.
- Positions (left/top) map to canvas x/y.
"""

import argparse
import json
import sys
import xml.etree.ElementTree as ET
from pathlib import Path


def iface_name(kind: str, adapter: int, port: int) -> str:
    prefix = "e" if kind == "eth" else "s"
    return f"{prefix}{adapter}/{port}"


def parse_unl(path: Path):
    tree = ET.parse(path)
    root = tree.getroot()

    lab_name = root.get("name") or path.stem
    nodes = []
    warnings = []

    # network_id -> list of (node_id, iface_str)
    net_members: dict[str, list] = {}

    for n in root.iterfind(".//node"):
        ntype = (n.get("type") or "").lower()
        nid = int(n.get("id"))
        name = n.get("name") or f"node{nid}"
        left = float(n.get("left") or 0)
        top = float(n.get("top") or 0)

        if ntype == "iol":
            eth = int(n.get("ethernet") or 1)
            ser = int(n.get("serial") or 1)
            image = n.get("image") or "UNKNOWN.bin"
            node = {
                "id": nid,
                "kind": "iol",
                "name": name,
                "x": left,
                "y": top,
                "image": {"id": "REPLACE_ON_IMPORT", "filename": image, "class": "unknown"},
                "ram": int(n.get("ram") or 256),
                "ethernet": eth,
                "serial": ser,
                "startupConfig": "",
            }
            nodes.append(node)
        elif ntype in ("vpcs", "docker") and "vpc" in name.lower():
            nodes.append({
                "id": nid, "kind": "vpcs", "name": name,
                "x": left, "y": top, "ethernet": 1, "serial": 0,
            })
        else:
            warnings.append(f"skipped unsupported node {nid} '{name}' type={ntype!r}")
            continue

        # collect interface -> network membership
        for itf in n.iterfind("./interface"):
            net_id = itf.get("network_id")
            if not net_id or net_id == "0":
                continue
            # EVE encodes adapter/port in the interface name or id; fall back to id
            iname = (itf.get("name") or "").lower()
            kind = "eth"
            adapter = port = 0
            # names like "e0/1" or "Ethernet0/1" / "s1/0"
            digits = iname.replace("ethernet", "e").replace("serial", "s")
            if digits.startswith("s"):
                kind = "ser"
            try:
                ap = digits[1:]
                adapter, port = (int(x) for x in ap.split("/"))
            except Exception:
                iid = int(itf.get("id") or 0)
                adapter, port = divmod(iid, 4)
            net_members.setdefault(net_id, []).append((nid, iface_name(kind, adapter, port)))

    links = []
    lid = 0
    for net_id, members in sorted(net_members.items(), key=lambda kv: int(kv[0])):
        if len(members) < 2:
            warnings.append(f"network {net_id} has <2 members; skipped")
            continue
        endpoints = [{"node": m[0], "interface": m[1]} for m in members]
        links.append({
            "id": lid,
            "type": "p2p" if len(members) == 2 else "segment",
            "endpoints": endpoints,
        })
        lid += 1

    lab = {
        "version": 1,
        "id": f"import-{path.stem}",
        "name": lab_name,
        "description": f"Imported from {path.name}",
        "canvas": {"zoom": 1, "pan": {"x": 0, "y": 0}, "background": "dots"},
        "nodes": nodes,
        "links": links,
    }
    return lab, warnings


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("unl", type=Path)
    ap.add_argument("-o", "--out", type=Path)
    args = ap.parse_args()

    lab, warnings = parse_unl(args.unl)
    out = args.out or args.unl.with_suffix(".lab.json")
    out.write_text(json.dumps(lab, indent=2), encoding="utf-8")

    for w in warnings:
        print(f"  warn: {w}", file=sys.stderr)
    print(f"wrote {out} ({len(lab['nodes'])} nodes, {len(lab['links'])} links)")


if __name__ == "__main__":
    main()
