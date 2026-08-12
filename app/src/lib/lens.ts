import { inspectPacket, type ParsedPacket } from "./pcapng";

// This vocabulary intentionally does not reuse watcherStore.ProtoKey: the
// Watcher mirrors backend classifier labels byte-for-byte, while DHCP and HSRP
// are browser-only Lens decoders.
export type LensProto = "arp" | "dhcp" | "stp" | "cdp" | "ospf" | "hsrp" | "other";

export interface EndpointAttribView {
  endpointIndex: number;
  state: "single" | "ambiguous" | "none" | string;
  mac?: string;
}

export interface LensAttribution {
  endpoints: readonly { node: number }[];
  epAttrib?: readonly EndpointAttribView[];
  nodeName(nodeId: number): string;
}

export interface LensEvent {
  seq: number;
  tsMicros: number;
  proto: LensProto;
  text: string;
  srcMac: string;
  dstMac: string;
  src: { node: number; name: string } | null;
  vlan: number | null;
  srcIp?: string;
  dstIp?: string;
}

const DHCP_TYPES: Record<number, string> = {
  1: "Discover",
  2: "Offer",
  3: "Request",
  4: "Decline",
  5: "ACK",
  6: "NAK",
  7: "Release",
  8: "Inform",
};

const HSRP_STATES: Record<number, string> = {
  0: "Initial",
  1: "Learn",
  2: "Listen",
  4: "Speak",
  8: "Standby",
  16: "Active",
};

const HSRP_OPCODES: Record<number, string> = {
  0: "Hello",
  1: "Coup",
  2: "Resign",
};

const OSPF_TYPES: Record<number, string> = {
  1: "Hello",
  2: "DB Description",
  3: "LS Request",
  4: "LS Update",
  5: "LS Ack",
};

function ipFrom(d: Uint8Array, o: number): string {
  return `${d[o]}.${d[o + 1]}.${d[o + 2]}.${d[o + 3]}`;
}

function u32(d: Uint8Array, o: number): number {
  return d[o] * 0x1000000 + (d[o + 1] << 16) + (d[o + 2] << 8) + d[o + 3];
}

function resolveSource(srcMac: string, attrib: LensAttribution): { node: number; name: string } | null {
  if (!srcMac || !attrib.epAttrib) return null;
  const matches = attrib.epAttrib.filter(
    (entry) => entry.state === "single" && entry.mac?.toLowerCase() === srcMac.toLowerCase()
  );
  if (matches.length !== 1) return null;
  const endpoint = attrib.endpoints[matches[0].endpointIndex];
  if (!endpoint) return null;
  return { node: endpoint.node, name: attrib.nodeName(endpoint.node) };
}

function decodeDhcp(frame: Uint8Array, payload: number): string | null {
  // BOOTP fixed header (236 bytes) + DHCP magic cookie (4 bytes).
  if (payload < 0 || frame.length < payload + 244) return null;
  if (u32(frame, payload + 236) !== 0x63825363) return null;

  const yiaddr = ipFrom(frame, payload + 16);
  let messageType: number | undefined;
  let serverId: string | undefined;
  let p = payload + 240;
  while (p < frame.length) {
    const code = frame[p++];
    if (code === 0) continue; // pad
    if (code === 255) break; // end
    if (p >= frame.length) break;
    const length = frame[p++];
    if (p + length > frame.length) break;
    if (code === 53 && length >= 1) messageType = frame[p];
    if (code === 54 && length >= 4) serverId = ipFrom(frame, p);
    p += length;
  }
  if (!messageType) return "DHCP";
  const label = DHCP_TYPES[messageType] ?? `message ${messageType}`;
  // Read server-id as part of the option walk even though v1's compact prose
  // only needs yiaddr; this keeps the parser ready for a future richer line.
  void serverId;
  if (messageType === 2) return `DHCP Offer ${yiaddr}`;
  if (messageType === 5) return `DHCP ACK ${yiaddr}`;
  if (messageType === 1 || messageType === 3) return `DHCP ${label}`;
  if (messageType === 6) return "DHCP NAK";
  return `DHCP ${label}`;
}

function decodeHsrp(frame: Uint8Array, payload: number): string | null {
  if (payload < 0 || frame.length < payload + 20) return null;
  const version = frame[payload];
  if (version !== 0) return "HSRP (v2)";
  const opcode = HSRP_OPCODES[frame[payload + 1]] ?? `opcode ${frame[payload + 1]}`;
  const state = HSRP_STATES[frame[payload + 2]] ?? `state ${frame[payload + 2]}`;
  const group = frame[payload + 6];
  const vip = ipFrom(frame, payload + 16);
  return `HSRP ${opcode} · group ${group} · ${state} · vIP ${vip}`;
}

function eventText(details: ReturnType<typeof inspectPacket>, frame: Uint8Array): { proto: LensProto; text: string } {
  const summary = details.summary;
  if (summary.proto === "ARP") return { proto: "arp", text: `ARP · ${summary.info || "frame"}` };
  if (summary.proto === "STP") return { proto: "stp", text: `STP · ${summary.info || "BPDU"}` };
  if (summary.proto === "CDP") return { proto: "cdp", text: `CDP${summary.info ? ` · ${summary.info}` : ""}` };

  if (
    details.ipProtocol === 17 &&
    details.l4PayloadOffset !== undefined &&
    details.srcPort !== undefined &&
    details.dstPort !== undefined
  ) {
    const isDhcp = details.srcPort === 67 || details.srcPort === 68 || details.dstPort === 67 || details.dstPort === 68;
    if (isDhcp) {
      const text = decodeDhcp(frame, details.l4PayloadOffset);
      if (text) return { proto: "dhcp", text };
    }
    const isHsrp = [1985, 2029].includes(details.srcPort) || [1985, 2029].includes(details.dstPort);
    if (isHsrp) {
      return { proto: "hsrp", text: decodeHsrp(frame, details.l4PayloadOffset) ?? "HSRP" };
    }
  }

  if (summary.proto === "OSPF") {
    const type = details.l4Offset !== undefined && frame.length > details.l4Offset + 1
      ? OSPF_TYPES[frame[details.l4Offset + 1]]
      : undefined;
    return { proto: "ospf", text: `OSPF ${type ?? "packet"}` };
  }

  const suffix = summary.info ? ` · ${summary.info}` : "";
  return { proto: "other", text: `${summary.proto}${suffix}` };
}

/** Build one event from an already-parsed packet. */
export function lensEvent(pkt: ParsedPacket, seq: number, attrib: LensAttribution): LensEvent | null {
  const details = inspectPacket(pkt.data, pkt.origLen);
  if (!details.srcMac || !details.dstMac) return null;
  const kind = eventText(details, pkt.data);
  return {
    seq,
    tsMicros: pkt.tsMicros,
    proto: kind.proto,
    text: kind.text,
    srcMac: details.srcMac,
    dstMac: details.dstMac,
    src: resolveSource(details.srcMac, attrib),
    vlan: details.vlan,
    srcIp: details.srcIp,
    dstIp: details.dstIp,
  };
}

/** Append a packet batch to the bounded per-link Lens ring. */
export function appendLensEvents(
  current: readonly LensEvent[],
  packets: readonly ParsedPacket[],
  firstSeq: number,
  attrib: LensAttribution,
): LensEvent[] {
  const next = [...current];
  packets.forEach((pkt, index) => {
    const event = lensEvent(pkt, firstSeq + index, attrib);
    if (event) next.push(event);
  });
  return next.slice(-2000);
}
