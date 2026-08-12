import { labStore } from "./labStore.svelte";
import type { NodeKind } from "./labTypes";

export interface CatalogEntry {
  id: string;
  group: "Devices" | "Endpoints" | "Services" | "IOL images";
  name: string;
  sub: string;
  icon: string;
  search: string;
  drag: { kind: NodeKind; imageId?: string; packId?: string };
  disabled?: string;
}

export function nodeCatalog(): CatalogEntry[] {
  const toolPacks = labStore.toolPacks;
  const defaultToolPack =
    ["webserver", "aaa", "httpclient"].find((id) => toolPacks.some((p) => p.id === id)) ??
    toolPacks[0]?.id;
  const hasNat = labStore.features.includes("natgw");
  const natSlirp = labStore.egress === "slirp";
  const natEgressNote =
    labStore.egressNote ||
    "DHCP & TCP only — no ping/traceroute on this runtime (QEMU slirp). Use the bridged VMware/OVA appliance or WSL2 for real internet.";

  const entries: CatalogEntry[] = [
    {
      id: "vpcs",
      group: "Endpoints",
      name: "VPCS",
      sub: "Virtual PC",
      icon: "pc",
      search: "vpcs virtual pc".toLowerCase(),
      drag: { kind: "vpcs" },
    },
    {
      id: "pc",
      group: "Endpoints",
      name: "PC",
      sub: "Netprobe",
      icon: "pc",
      search: "pc netprobe virtual pc addressing ping traceroute dns tcp udp".toLowerCase(),
      drag: { kind: "pc" },
    },
  ];

  if (toolPacks.length > 0 && defaultToolPack) {
    entries.push({
      id: "tool",
      group: "Services",
      name: "Network tools",
      sub: "Learning tool",
      icon: "tool",
      search: "network tools learning tool".toLowerCase(),
      drag: { kind: "tool", packId: defaultToolPack },
    });
  }

  if (hasNat) {
    entries.push({
      id: "nat",
      group: "Services",
      name: "NAT Gateway",
      sub: natSlirp ? natEgressNote : "Internet egress",
      icon: "nat",
      search: `nat gateway ${natSlirp ? natEgressNote : "internet egress"}`.toLowerCase(),
      drag: { kind: "nat" },
    });
  }

  for (const img of labStore.images) {
    const isL2 = img.class === "l2";
    const name = isL2 ? "Switch" : "Router";
    const sub = `${img.class.toUpperCase()} · ${img.arch}`;
    entries.push({
      id: `iol:${img.id}`,
      group: "IOL images",
      name,
      sub,
      icon: isL2 ? "switch" : "router",
      search: `${name} ${sub} ${img.filename} ${img.arch} ${img.class}`.toLowerCase(),
      drag: { kind: "iol", imageId: img.id },
    });
  }

  return entries;
}
