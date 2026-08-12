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

// Pack manifests share a small icon vocabulary, so keep the catalog mapping
// explicit where two packs would otherwise inherit the same generic glyph.
const TOOL_PACK_ICONS: Record<string, string> = {
  aaa: "firewall",
  httpclient: "cloud",
  netsvc: "services",
  secbench: "l3-switch",
  syslog: "syslog",
  webserver: "server",
};

const TOOL_PACK_SUBS: Record<string, string> = {
  aaa: "Authentication",
  httpclient: "HTTP requests",
  netsvc: "DHCP · DNS · NTP",
  secbench: "Security testing",
  syslog: "Log collector",
  webserver: "HTTP service",
};

const TOOL_PACK_SEARCH_TERMS: Record<string, string> = {
  aaa: "aaa server authentication authorization accounting radius tacacs",
  httpclient: "http client requests web fetch curl",
  netsvc: "network services dhcp dns ntp tftp",
  secbench: "security bench reconnaissance arp spoof dhcp stp vlan fhrp",
  syslog: "syslog server logging logs collector",
  webserver: "web server http https service",
};

function groupLabel(group: string): string {
  return group.replace(/[-_]+/g, " ").replace(/\b\w/g, (letter) => letter.toUpperCase());
}

function toolPackIcon(pack: { id: string; icon: string }): string {
  return TOOL_PACK_ICONS[pack.id] ?? (pack.icon || "tool");
}

export function nodeCatalog(): CatalogEntry[] {
  const toolPacks = labStore.toolPacks;
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

  for (const pack of toolPacks) {
    if (pack.id === "stub") continue; // Internal test fixture, not user-facing.
    const sub =
      TOOL_PACK_SUBS[pack.id] ??
      (pack.groups.length > 0 ? groupLabel(pack.groups[0]) : "Learning tool");
    const searchTerms =
      TOOL_PACK_SEARCH_TERMS[pack.id] ??
      (pack.groups.length > 0 ? pack.groups.join(" ") : "learning tool");
    entries.push({
      id: `tool:${pack.id}`,
      group: "Services",
      name: pack.name,
      sub,
      icon: toolPackIcon(pack),
      search: `${pack.name} ${searchTerms}`.toLowerCase(),
      drag: { kind: "tool", packId: pack.id },
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
