// Incremental pcapng parser + a light packet summarizer, feeding the live
// capture console tab (feature 1). Bytes arrive over a WebSocket in arbitrary
// chunk boundaries, so the parser buffers partial blocks and yields only whole
// packets.
//
// pcapng structure we care about (see the pcapng spec):
//   - Every block: u32 type | u32 total-length | body | u32 total-length (again).
//     total-length is padded to a 4-byte multiple and INCLUDES both length
//     fields (so minimum 12 bytes).
//   - SHB (0x0A0D0D0A) carries the byte-order magic (0x1A2B3C4D). We read it
//     RAW (it must be interpreted in native order to *discover* endianness):
//     0x1A2B3C4D little-endian on the wire → LE; else BE. All later reads use
//     the discovered endianness.
//   - IDB (0x00000001) declares an interface; we track its LinkType (usually
//     ETHERNET=1) and tsresol so timestamps decode, but emit nothing.
//   - EPB (0x00000006): interface-id | ts-high | ts-low | captured-len |
//     original-len | packet-data. This is the frame we summarize.
//   - SPB (0x00000003, simple packet): original-len | data. No timestamp.
//
// The parser never blocks on a partial block: push() appends to an internal
// buffer, then drains as many complete blocks as are fully present, returning
// one ParsedPacket per EPB/SPB. Incomplete trailing bytes stay buffered for the
// next push().

export interface ParsedPacket {
  /** Monotonic packet index within the stream (1-based). */
  index: number;
  /** Seconds since capture start (relative to the first packet). */
  tRel: number;
  /** Absolute capture time in microseconds since the Unix epoch — preserved so
   *  the "Save .pcapng" download can re-serialize faithful timestamps. */
  tsMicros: number;
  /** Captured frame bytes (may be shorter than origLen if snaplen truncated). */
  data: Uint8Array;
  /** Original on-wire length. */
  origLen: number;
}

const BT_SHB = 0x0a0d0d0a;
const BT_IDB = 0x00000001;
const BT_SPB = 0x00000003;
const BT_EPB = 0x00000006;
const BYTE_ORDER_MAGIC = 0x1a2b3c4d;

export class PcapngParser {
  private buf = new Uint8Array(0);
  private little = true;
  /** tsresol: nanoseconds-per-tick denominator. Default 1e6 (microseconds). */
  private tsPerSec = 1_000_000;
  private index = 0;
  private t0: number | null = null;

  /** Append a chunk and return every packet that became complete. */
  push(chunk: Uint8Array): ParsedPacket[] {
    // Append. (Concatenate rather than a ring buffer — capture chunks are small
    // and this keeps the block boundary logic trivial.)
    if (this.buf.length === 0) {
      this.buf = chunk.slice();
    } else {
      const merged = new Uint8Array(this.buf.length + chunk.length);
      merged.set(this.buf, 0);
      merged.set(chunk, this.buf.length);
      this.buf = merged;
    }

    const out: ParsedPacket[] = [];
    let off = 0;
    const view = () => new DataView(this.buf.buffer, this.buf.byteOffset, this.buf.byteLength);

    while (this.buf.length - off >= 12) {
      const dv = view();
      const blockType = dv.getUint32(off, this.little);
      // SHB is special: its type is endian-neutral (palindrome), but its length
      // must be read in the endianness we're about to discover. Peek the magic.
      let totalLen: number;
      if (blockType === BT_SHB) {
        // Byte-order magic is at off+8. Read it big-endian-agnostic: try LE.
        const magicLE = dv.getUint32(off + 8, true);
        this.little = magicLE === BYTE_ORDER_MAGIC;
        totalLen = dv.getUint32(off + 4, this.little);
      } else {
        totalLen = dv.getUint32(off + 4, this.little);
      }

      // Guard against a corrupt/mis-synced length (would hang or over-read).
      if (totalLen < 12 || totalLen > 8 * 1024 * 1024) {
        // Unrecoverable framing — drop everything and resync on the next SHB-ish
        // boundary would be complex; simplest safe move is to reset the buffer.
        this.buf = new Uint8Array(0);
        return out;
      }
      if (this.buf.length - off < totalLen) break; // incomplete block; wait

      const bodyStart = off + 8;
      switch (blockType) {
        case BT_SHB:
          // Endianness already set; nothing else needed for summaries.
          break;
        case BT_IDB:
          this.parseIdb(dv, bodyStart, off + totalLen - 4);
          break;
        case BT_EPB: {
          const pkt = this.parseEpb(dv, bodyStart);
          if (pkt) out.push(pkt);
          break;
        }
        case BT_SPB: {
          const pkt = this.parseSpb(dv, bodyStart, totalLen);
          if (pkt) out.push(pkt);
          break;
        }
        default:
          break; // NRB, DSB, custom blocks etc. — skip.
      }
      off += totalLen;
    }

    // Retain the unconsumed tail.
    this.buf = off > 0 ? this.buf.slice(off) : this.buf;
    return out;
  }

  private parseIdb(dv: DataView, bodyStart: number, optionsEnd: number) {
    // IDB body: linktype(u16) reserved(u16) snaplen(u32) then options.
    // We scan options for if_tsresol (code 9) to decode timestamps precisely.
    let p = bodyStart + 8;
    while (p + 4 <= optionsEnd) {
      const code = dv.getUint16(p, this.little);
      const len = dv.getUint16(p + 2, this.little);
      p += 4;
      if (code === 0) break; // opt_endofopt
      if (code === 9 && len >= 1) {
        const raw = dv.getUint8(p);
        // High bit set → power of two; else power of ten. Value is the exponent.
        if (raw & 0x80) this.tsPerSec = Math.pow(2, raw & 0x7f);
        else this.tsPerSec = Math.pow(10, raw);
      }
      p += len + ((4 - (len % 4)) % 4); // pad to 4
    }
  }

  private parseEpb(dv: DataView, bodyStart: number): ParsedPacket | null {
    // EPB body: iface(u32) ts_high(u32) ts_low(u32) caplen(u32) origlen(u32) data.
    const tsHigh = dv.getUint32(bodyStart + 4, this.little);
    const tsLow = dv.getUint32(bodyStart + 8, this.little);
    const capLen = dv.getUint32(bodyStart + 12, this.little);
    const origLen = dv.getUint32(bodyStart + 16, this.little);
    const dataStart = bodyStart + 20;
    const ticks = tsHigh * 0x1_0000_0000 + tsLow;
    const tAbs = ticks / this.tsPerSec;
    return this.emit(tAbs, dv, dataStart, capLen, origLen);
  }

  private parseSpb(dv: DataView, bodyStart: number, totalLen: number): ParsedPacket | null {
    // SPB body: origlen(u32) data. No timestamp, no caplen — caplen is inferred
    // from the block length (total - 4 length fields - 4 origlen - trailing len).
    const origLen = dv.getUint32(bodyStart, this.little);
    const dataStart = bodyStart + 4;
    const capLen = totalLen - 16; // 8 header + 4 origlen + 4 trailing len
    return this.emit(0, dv, dataStart, Math.max(0, capLen), origLen);
  }

  private emit(
    tAbs: number,
    dv: DataView,
    dataStart: number,
    capLen: number,
    origLen: number
  ): ParsedPacket {
    if (this.t0 === null) this.t0 = tAbs;
    const tRel = Math.max(0, tAbs - this.t0);
    const data = new Uint8Array(dv.buffer, dv.byteOffset + dataStart, capLen).slice();
    this.index += 1;
    return { index: this.index, tRel, tsMicros: Math.round(tAbs * 1_000_000), data, origLen };
  }
}

// ---------------------------------------------------------------------------
// pcapng ENCODER — re-serialize captured packets into a fresh, single-section
// pcapng file for the "Save .pcapng" download.
//
// Why not just save the raw bytes we received? The live /capture WS stream can
// span RECONNECTS (each restarts with its own Section Header Block) and a drop
// can land mid-block, so concatenating the raw byte chunks produces a file with
// a truncated block wedged before a second SHB — which Wireshark rejects
// ("isn't a capture file in a format Wireshark understands"). Rebuilding from
// the already-PARSED packets guarantees ONE clean section: SHB +
// IDB(LINKTYPE_ETHERNET) + one Enhanced Packet Block per packet, with faithful
// absolute-microsecond timestamps. Mirrors supervisor/internal/relay/pcapng.go.
// ---------------------------------------------------------------------------

export interface CapturedPacket {
  data: Uint8Array;
  tsMicros: number;
}

export function encodePcapng(packets: CapturedPacket[]): Uint8Array {
  const LINKTYPE_ETHERNET = 1;
  const SNAPLEN = 262144;

  let total = 28 + 20; // SHB(28) + IDB(20)
  for (const p of packets) {
    const pad = (4 - (p.data.length % 4)) % 4;
    total += 32 + p.data.length + pad; // EPB: 32 header/trailer + data + pad
  }

  const out = new Uint8Array(total);
  const dv = new DataView(out.buffer);
  let o = 0;
  const u32 = (v: number) => {
    dv.setUint32(o, v >>> 0, true);
    o += 4;
  };
  const u16 = (v: number) => {
    dv.setUint16(o, v & 0xffff, true);
    o += 2;
  };

  // Section Header Block (little-endian).
  u32(0x0a0d0d0a);
  u32(28);
  u32(0x1a2b3c4d); // byte-order magic
  u16(1);
  u16(0); // major, minor
  u32(0xffffffff);
  u32(0xffffffff); // section length = -1 (unknown)
  u32(28);

  // Interface Description Block.
  u32(0x00000001);
  u32(20);
  u16(LINKTYPE_ETHERNET);
  u16(0); // reserved
  u32(SNAPLEN);
  u32(20);

  // One Enhanced Packet Block per packet.
  for (const p of packets) {
    const capLen = p.data.length;
    const pad = (4 - (capLen % 4)) % 4;
    const epbTotal = 32 + capLen + pad;
    const ts = Math.max(0, Math.floor(p.tsMicros));
    u32(0x00000006);
    u32(epbTotal);
    u32(0); // interface id 0
    u32(Math.floor(ts / 0x1_0000_0000)); // ts high
    u32(ts >>> 0); // ts low (mod 2^32)
    u32(capLen);
    u32(capLen);
    out.set(p.data, o);
    o += capLen + pad; // trailing pad bytes are already zero
    u32(epbTotal);
  }
  return out;
}

// ---------------------------------------------------------------------------
// Summarizer — a tshark-ish one-liner per packet. Ethernet II framing, then
// IPv4/IPv6/ARP dissection down to TCP/UDP/ICMP; falls back to MAC + ethertype
// when there's no IP.
// ---------------------------------------------------------------------------

export interface PacketSummary {
  /** Protocol label used for the colored column (e.g. "TCP", "ICMP", "ARP"). */
  proto: string;
  /** "src > dst" — IPs (with ports for TCP/UDP) or MACs when no L3. */
  addr: string;
  /** Extra info (flags, ICMP type, ethertype fallback). */
  info: string;
  /** L2/L3 length in bytes (original on-wire length). */
  len: number;
}

function hex2(b: number): string {
  return b.toString(16).padStart(2, "0");
}

function mac(d: Uint8Array, o: number): string {
  return `${hex2(d[o])}:${hex2(d[o + 1])}:${hex2(d[o + 2])}:${hex2(d[o + 3])}:${hex2(d[o + 4])}:${hex2(d[o + 5])}`;
}

function ipv4(d: Uint8Array, o: number): string {
  return `${d[o]}.${d[o + 1]}.${d[o + 2]}.${d[o + 3]}`;
}

function ipv6(d: Uint8Array, o: number): string {
  const parts: string[] = [];
  for (let i = 0; i < 16; i += 2) parts.push(((d[o + i] << 8) | d[o + i + 1]).toString(16));
  // Light "::" compression of the longest zero run.
  let best = -1,
    bestLen = 0,
    cur = -1,
    curLen = 0;
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] === "0") {
      if (cur < 0) cur = i;
      curLen++;
      if (curLen > bestLen) {
        best = cur;
        bestLen = curLen;
      }
    } else {
      cur = -1;
      curLen = 0;
    }
  }
  if (bestLen > 1) {
    const head = parts.slice(0, best).join(":");
    const tail = parts.slice(best + bestLen).join(":");
    return `${head}::${tail}`;
  }
  return parts.join(":");
}

const ETH_ETHERTYPES: Record<number, string> = {
  0x0800: "IPv4",
  0x0806: "ARP",
  0x86dd: "IPv6",
  0x8100: "802.1Q",
  0x88cc: "LLDP",
  0x8847: "MPLS",
  0x9000: "LOOP", // Ethernet Configuration Testing Protocol (keepalive/loopback)
};

const ICMP_TYPES: Record<number, string> = {
  0: "Echo reply",
  3: "Destination unreachable",
  5: "Redirect",
  8: "Echo request",
  11: "Time exceeded",
  13: "Timestamp",
  14: "Timestamp reply",
};

const ICMP6_TYPES: Record<number, string> = {
  128: "Echo request",
  129: "Echo reply",
  133: "Router solicitation",
  134: "Router advertisement",
  135: "Neighbor solicitation",
  136: "Neighbor advertisement",
};

function u16(d: Uint8Array, o: number): number {
  return (d[o] << 8) | d[o + 1];
}

function tcpFlags(d: Uint8Array, o: number): string {
  const f = d[o + 13];
  const names: string[] = [];
  if (f & 0x02) names.push("SYN");
  if (f & 0x10) names.push("ACK");
  if (f & 0x01) names.push("FIN");
  if (f & 0x04) names.push("RST");
  if (f & 0x08) names.push("PSH");
  if (f & 0x20) names.push("URG");
  return names.length ? `[${names.join(" ")}]` : "";
}

/** Summarize one ethernet frame. Defensive against truncated captures. */
export function summarize(frame: Uint8Array, origLen: number): PacketSummary {
  const len = origLen || frame.length;
  if (frame.length < 14) {
    return { proto: "?", addr: "", info: "runt frame", len };
  }
  const dstMac = mac(frame, 0);
  const srcMac = mac(frame, 6);
  let etherType = u16(frame, 12);
  let l3 = 14;
  // Single VLAN tag hop (802.1Q).
  if (etherType === 0x8100 && frame.length >= 18) {
    etherType = u16(frame, 16);
    l3 = 18;
  }

  // 802.3 framing: a value below 0x0600 (1536) is a LENGTH field, not an
  // EtherType. The old code fell straight through to the hex fallback, so real
  // STP BPDUs (dst 01:80:c2:00:00:00) showed up as "0x0027" and CDP/DTP/VTP over
  // SNAP (dst 01:00:0c:cc:cc:cc) as "0x0183"/"0x004c" etc. Decode LLC instead.
  if (etherType < 0x0600) {
    return summarizeLlc(frame, l3, srcMac, dstMac, len);
  }

  return dispatchEtherType(frame, etherType, l3, srcMac, dstMac, len);
}

/** Dispatch by EtherType. Shared by Ethernet II framing and by SNAP frames
 *  whose OUI is 00:00:00 (which carry a real EtherType in the SNAP PID). */
function dispatchEtherType(
  frame: Uint8Array,
  etherType: number,
  l3: number,
  srcMac: string,
  dstMac: string,
  len: number
): PacketSummary {
  if (etherType === 0x0806) {
    // ARP
    if (frame.length >= l3 + 28) {
      const op = u16(frame, l3 + 6);
      const spa = ipv4(frame, l3 + 14);
      const tpa = ipv4(frame, l3 + 24);
      const info =
        op === 1 ? `Who has ${tpa}? Tell ${spa}` : op === 2 ? `${spa} is at ${mac(frame, l3 + 8)}` : `op ${op}`;
      return { proto: "ARP", addr: `${spa} > ${tpa}`, info, len };
    }
    return { proto: "ARP", addr: `${srcMac} > ${dstMac}`, info: "", len };
  }

  if (etherType === 0x0800) {
    return summarizeIPv4(frame, l3, len);
  }
  if (etherType === 0x86dd) {
    return summarizeIPv6(frame, l3, len);
  }

  // No L3 we dissect: MACs + ethertype hex fallback.
  const label = ETH_ETHERTYPES[etherType] ?? `0x${etherType.toString(16).padStart(4, "0")}`;
  return { proto: label, addr: `${srcMac} > ${dstMac}`, info: "", len };
}

const ascii = new TextDecoder("ascii", { fatal: false });

/** Read a run of printable ASCII (used for the CDP Device ID string). */
function asciiStr(d: Uint8Array, o: number, n: number): string {
  return ascii.decode(d.subarray(o, o + n)).replace(/[^\x20-\x7e]/g, "");
}

/**
 * 802.3 LLC frame (the L2 "type" field was actually a length < 0x0600). `o` is
 * the offset just past the length field, where the 802.2 LLC header begins:
 * DSAP(1) SSAP(1) control(1|2). Decodes the protocols IOS labs emit onto the
 * capture: STP (DSAP 0x42), and SNAP (AA AA 03) for CDP/DTP/VTP.
 */
function summarizeLlc(
  frame: Uint8Array,
  o: number,
  srcMac: string,
  dstMac: string,
  len: number
): PacketSummary {
  if (frame.length < o + 3) {
    return { proto: "LLC", addr: `${srcMac} > ${dstMac}`, info: "runt", len };
  }
  const dsap = frame[o];
  const ssap = frame[o + 1];

  // Spanning Tree (802.1D/w/s): DSAP=SSAP=0x42, control 0x03 (UI). BPDU follows.
  if (dsap === 0x42 && ssap === 0x42) {
    return summarizeStp(frame, o + 3, srcMac, dstMac, len);
  }

  // SNAP: DSAP=SSAP=0xAA, control 0x03, then OUI(3) + PID(2).
  if (dsap === 0xaa && ssap === 0xaa && frame.length >= o + 8) {
    const oui = (frame[o + 3] << 16) | (frame[o + 4] << 8) | frame[o + 5];
    const pid = u16(frame, o + 6);
    const snapBody = o + 8;
    if (oui === 0x00000c) {
      // Cisco SNAP protocols.
      if (pid === 0x2000) return summarizeCdp(frame, snapBody, srcMac, dstMac, len);
      if (pid === 0x2004) return { proto: "DTP", addr: `${srcMac} > ${dstMac}`, info: "", len };
      if (pid === 0x2003) return { proto: "VTP", addr: `${srcMac} > ${dstMac}`, info: "", len };
      return { proto: "SNAP", addr: `${srcMac} > ${dstMac}`, info: `Cisco pid 0x${hex2(frame[o + 6])}${hex2(frame[o + 7])}`, len };
    }
    if (oui === 0x000000) {
      // OUI 00:00:00 → the SNAP PID is a real EtherType; reuse the L2 path.
      return dispatchEtherType(frame, pid, snapBody, srcMac, dstMac, len);
    }
    return { proto: "SNAP", addr: `${srcMac} > ${dstMac}`, info: `oui ${hex2(frame[o + 3])}:${hex2(frame[o + 4])}:${hex2(frame[o + 5])}`, len };
  }

  // Any other LLC frame: report the SAPs.
  return { proto: "LLC", addr: `${srcMac} > ${dstMac}`, info: `dsap 0x${hex2(dsap)} ssap 0x${hex2(ssap)}`, len };
}

/**
 * STP/RSTP/MST BPDU. `o` points at the BPDU: protocol-id(u16, 0x0000)
 * version(u8) type(u8) ... For a config BPDU the root bridge id and path cost
 * follow. A TCN BPDU (type 0x80) has no further fields.
 */
function summarizeStp(
  frame: Uint8Array,
  o: number,
  srcMac: string,
  dstMac: string,
  len: number
): PacketSummary {
  const addr = `${srcMac} > ${dstMac}`;
  if (frame.length < o + 4) return { proto: "STP", addr, info: "runt", len };
  const bpduType = frame[o + 3];
  if (bpduType === 0x80) {
    return { proto: "STP", addr, info: "Topology change", len };
  }
  const version = frame[o + 2];
  const kind = version === 3 ? "MST" : version === 2 ? "RSTP" : "STP";
  // Config/RSTP/MST BPDU: flags(1) at o+4, root-id(8) at o+5, root-path-cost(4) at o+13.
  if (frame.length < o + 17) return { proto: "STP", addr, info: kind, len };
  const rootPrio = u16(frame, o + 5); // priority + sys-id-ext (kept as one u16)
  const rootMac = mac(frame, o + 7);
  const cost = (frame[o + 13] << 24) | (frame[o + 14] << 16) | (frame[o + 15] << 8) | frame[o + 16];
  return { proto: "STP", addr, info: `${kind} · root ${rootPrio}/${rootMac} cost ${cost >>> 0}`, len };
}

/**
 * CDP. `o` points past the SNAP header at the CDP header: version(1) ttl(1)
 * checksum(u16), then TLVs: type(u16) length(u16) value. We pull the Device ID
 * TLV (type 0x0001) if present; length includes the 4-byte TLV header.
 */
function summarizeCdp(
  frame: Uint8Array,
  o: number,
  srcMac: string,
  dstMac: string,
  len: number
): PacketSummary {
  const addr = `${srcMac} > ${dstMac}`;
  let p = o + 4; // skip CDP header (version, ttl, checksum)
  while (p + 4 <= frame.length) {
    const type = u16(frame, p);
    const tlvLen = u16(frame, p + 2);
    if (tlvLen < 4) break; // malformed; avoid a zero/negative-length loop
    if (type === 0x0001) {
      const valLen = Math.min(tlvLen - 4, frame.length - (p + 4));
      const devId = asciiStr(frame, p + 4, Math.max(0, valLen));
      return { proto: "CDP", addr, info: devId ? `Device ID ${devId}` : "", len };
    }
    p += tlvLen;
  }
  return { proto: "CDP", addr, info: "", len };
}

function summarizeIPv4(frame: Uint8Array, o: number, len: number): PacketSummary {
  if (frame.length < o + 20) return { proto: "IPv4", addr: "", info: "truncated", len };
  const ihl = (frame[o] & 0x0f) * 4;
  const proto = frame[o + 9];
  const src = ipv4(frame, o + 12);
  const dst = ipv4(frame, o + 16);
  const l4 = o + ihl;
  return summarizeL4(frame, proto, src, dst, l4, len, false);
}

function summarizeIPv6(frame: Uint8Array, o: number, len: number): PacketSummary {
  if (frame.length < o + 40) return { proto: "IPv6", addr: "", info: "truncated", len };
  const nextHdr = frame[o + 6];
  const src = ipv6(frame, o + 8);
  const dst = ipv6(frame, o + 24);
  const l4 = o + 40;
  return summarizeL4(frame, nextHdr, src, dst, l4, len, true);
}

function summarizeL4(
  frame: Uint8Array,
  proto: number,
  src: string,
  dst: string,
  l4: number,
  len: number,
  v6: boolean
): PacketSummary {
  // TCP
  if (proto === 6 && frame.length >= l4 + 14) {
    const sport = u16(frame, l4);
    const dport = u16(frame, l4 + 2);
    return { proto: "TCP", addr: `${src}:${sport} > ${dst}:${dport}`, info: tcpFlags(frame, l4), len };
  }
  // UDP
  if (proto === 17 && frame.length >= l4 + 8) {
    const sport = u16(frame, l4);
    const dport = u16(frame, l4 + 2);
    return { proto: "UDP", addr: `${src}:${sport} > ${dst}:${dport}`, info: "", len };
  }
  // ICMP / ICMPv6
  if ((proto === 1 || proto === 58) && frame.length >= l4 + 1) {
    const type = frame[l4];
    const names = v6 ? ICMP6_TYPES : ICMP_TYPES;
    const label = proto === 58 ? "ICMPv6" : "ICMP";
    return { proto: label, addr: `${src} > ${dst}`, info: names[type] ?? `type ${type}`, len };
  }
  const names: Record<number, string> = {
    2: "IGMP",
    47: "GRE",
    50: "ESP",
    51: "AH",
    88: "EIGRP",
    89: "OSPF",
    103: "PIM",
    112: "VRRP",
  };
  const label = names[proto] ?? (v6 ? "IPv6" : "IPv4");
  return { proto: label, addr: `${src} > ${dst}`, info: v6 ? "" : `proto ${proto}`, len };
}
