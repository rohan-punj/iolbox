// Containerlab (.clab.yml) interop: import a containerlab topology into an iolab
// lab document, and export the current lab as a containerlab file. iolab's own
// store stays JSON; this is a format bridge so the (much more readable) clab YAML
// can be round-tripped.
//
// The supported clab subset (the real-world shape, e.g. networklessons' labs):
//
//   name: <labname>
//   topology:
//     kinds:
//       cisco_iol: { image: ... }        # ignored — iolab binds images by content id
//     nodes:
//       R1: { kind: cisco_iol, startup-config: ./..., image: ... }
//     links:
//       - endpoints: ["R1:Ethernet0/1","R2:Ethernet0/1"]
//
// Mapping: cisco_iol/ios kinds -> iolab "iol"; linux/host/alpine -> "vpcs";
// anything else -> "iol" with a warning. clab image refs are docker registry
// paths (not portable), so imported IOL nodes come in UNBOUND — the user assigns
// an image from their library. startup-config file paths can't be read in the
// browser, so they are skipped with a warning. Nodes get an auto circle layout.

import { emptyLab, type LabDocument, type LabLink, type LabNode, type NodeKind } from "./labTypes";

// ---------------------------------------------------------------------------
// Minimal YAML-subset parser (block maps, block/flow sequences, scalars, #
// comments, quoted strings). Sufficient for containerlab topologies; not a
// general YAML implementation (no anchors, multi-line scalars, etc.).
// ---------------------------------------------------------------------------

type Line = { indent: number; text: string };

function stripComment(line: string): string {
  // Remove a trailing # comment, but not one inside a quoted string.
  let inS = "";
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (inS) {
      if (c === inS) inS = "";
    } else if (c === '"' || c === "'") {
      inS = c;
    } else if (c === "#" && (i === 0 || line[i - 1] === " " || line[i - 1] === "\t")) {
      return line.slice(0, i);
    }
  }
  return line;
}

function tokenize(src: string): Line[] {
  const out: Line[] = [];
  for (const raw of src.split(/\r?\n/)) {
    const noComment = stripComment(raw);
    if (noComment.trim() === "") continue;
    if (noComment.trim() === "---" || noComment.trim() === "...") continue;
    out.push({ indent: noComment.length - noComment.trimStart().length, text: noComment.trim() });
  }
  return out;
}

function splitFlowSeq(s: string): string[] {
  // "a, \"b:c\", 'd'" -> ["a", "b:c", "d"], respecting quotes.
  const parts: string[] = [];
  let cur = "";
  let inS = "";
  for (const c of s) {
    if (inS) {
      if (c === inS) inS = "";
      else cur += c;
    } else if (c === '"' || c === "'") {
      inS = c;
    } else if (c === ",") {
      parts.push(cur.trim());
      cur = "";
    } else {
      cur += c;
    }
  }
  if (cur.trim() !== "") parts.push(cur.trim());
  return parts;
}

function parseScalar(s: string): unknown {
  const t = s.trim();
  if (t === "") return null;
  if (t === "null" || t === "~") return null;
  if (t === "true") return true;
  if (t === "false") return false;
  if (t.startsWith("[") && t.endsWith("]")) {
    return splitFlowSeq(t.slice(1, -1)).map((x) => parseScalar(x));
  }
  if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'"))) {
    return t.slice(1, -1);
  }
  if (/^-?\d+$/.test(t)) return parseInt(t, 10);
  return t;
}

function isKeyValue(text: string): boolean {
  // A mapping entry "key: value" or "key:". Guard against a lone URL-ish scalar.
  const m = text.match(/^("[^"]*"|'[^']*'|[^:\s][^:]*?):(\s|$)/);
  return m !== null;
}

function splitKeyValue(text: string): { key: string; val: string } {
  const idx = text.indexOf(":");
  const key = text.slice(0, idx).trim().replace(/^['"]|['"]$/g, "");
  return { key, val: text.slice(idx + 1).trim() };
}

function parseYaml(src: string): unknown {
  const lines = tokenize(src);
  let pos = 0;

  function parseBlock(indent: number): unknown {
    if (pos >= lines.length) return null;
    // Sequence?
    if (lines[pos].indent === indent && (lines[pos].text === "-" || lines[pos].text.startsWith("- "))) {
      const arr: unknown[] = [];
      while (
        pos < lines.length &&
        lines[pos].indent === indent &&
        (lines[pos].text === "-" || lines[pos].text.startsWith("- "))
      ) {
        const cur = lines[pos];
        const rest = cur.text === "-" ? "" : cur.text.slice(2);
        if (rest === "") {
          pos++;
          arr.push(pos < lines.length && lines[pos].indent > indent ? parseBlock(lines[pos].indent) : null);
        } else if (isKeyValue(rest)) {
          // Inline map item ("- key: val"): reinterpret this line as a map key at
          // a deeper virtual indent so the map picks up any sibling keys below.
          lines[pos] = { indent: cur.indent + 2, text: rest };
          arr.push(parseBlock(cur.indent + 2));
        } else {
          pos++;
          arr.push(parseScalar(rest));
        }
      }
      return arr;
    }
    // Mapping?
    if (lines[pos].indent === indent && isKeyValue(lines[pos].text)) {
      const map: Record<string, unknown> = {};
      while (pos < lines.length && lines[pos].indent === indent && isKeyValue(lines[pos].text)) {
        const { key, val } = splitKeyValue(lines[pos].text);
        pos++;
        if (val === "") {
          map[key] = pos < lines.length && lines[pos].indent > indent ? parseBlock(lines[pos].indent) : null;
        } else {
          map[key] = parseScalar(val);
        }
      }
      return map;
    }
    // Bare scalar.
    const s = parseScalar(lines[pos].text);
    pos++;
    return s;
  }

  return lines.length ? parseBlock(lines[0].indent) : null;
}

// ---------------------------------------------------------------------------
// Import: containerlab topology -> iolab LabDocument.
// ---------------------------------------------------------------------------

function mapKind(clabKind: string): NodeKind {
  const k = (clabKind || "").toLowerCase();
  if (k.includes("iol") || k.includes("ios") || k.includes("cisco_") || k.includes("router")) return "iol";
  if (k.includes("linux") || k.includes("host") || k.includes("alpine") || k.includes("vpcs") || k.includes("client")) return "vpcs";
  return "iol";
}

// Normalise a clab interface name to iolab canonical form. IOL: "Ethernet0/1",
// "Eth0/1", "e0/1" all -> "e0/1". VPCS has a single interface -> "eth0".
function normIface(kind: NodeKind, raw: string): string {
  if (kind === "vpcs") return "eth0";
  const m = raw.match(/(\d+)\/(\d+)/);
  if (m) return `e${m[1]}/${m[2]}`;
  return "e0/0";
}

function parseEndpoint(ep: unknown): { name: string; iface: string } | null {
  if (typeof ep === "string") {
    const idx = ep.indexOf(":");
    if (idx < 0) return null;
    return { name: ep.slice(0, idx).trim(), iface: ep.slice(idx + 1).trim() };
  }
  if (ep && typeof ep === "object") {
    const o = ep as Record<string, unknown>;
    if (typeof o.node === "string" && typeof o.interface === "string") {
      return { name: o.node, iface: o.interface };
    }
  }
  return null;
}

export function importClab(text: string): { doc: LabDocument; warnings: string[] } {
  const y = parseYaml(text) as Record<string, unknown> | null;
  const warnings: string[] = [];
  if (!y || typeof y !== "object") throw new Error("could not parse YAML");

  const topo = (y.topology as Record<string, unknown>) || {};
  const nodesObj = (topo.nodes as Record<string, Record<string, unknown>>) || {};
  const nodeNames = Object.keys(nodesObj);
  if (nodeNames.length === 0) {
    throw new Error("no topology.nodes found — is this a containerlab .clab.yml file?");
  }

  const nameToId = new Map<string, number>();
  nodeNames.forEach((nm, i) => nameToId.set(nm, i));
  const kindOf = new Map<string, NodeKind>();

  // Resolve each node's kind (per-node kind, else topology.kinds is only a
  // defaults block keyed by kind name, so the node's own `kind` is authoritative).
  for (const nm of nodeNames) {
    const raw = nodesObj[nm] || {};
    const clabKind = String(raw.kind ?? "");
    const kind = mapKind(clabKind);
    kindOf.set(nm, kind);
    if (kind === "iol" && !/iol|ios|cisco_|router/i.test(clabKind)) {
      warnings.push(`node "${nm}": kind "${clabKind || "?"}" not recognised — imported as IOL`);
    }
  }

  // Adapter-group count per IOL node from the interfaces its links use.
  const maxAdapter = new Map<string, number>();
  const rawLinks = Array.isArray(topo.links) ? (topo.links as unknown[]) : [];
  for (const l of rawLinks) {
    const eps = (l as Record<string, unknown>)?.endpoints;
    if (!Array.isArray(eps)) continue;
    for (const ep of eps) {
      const p = parseEndpoint(ep);
      if (!p) continue;
      const m = p.iface.match(/(\d+)\/\d+/);
      if (m) maxAdapter.set(p.name, Math.max(maxAdapter.get(p.name) ?? 0, parseInt(m[1], 10)));
    }
  }

  // Auto circle layout (clab has no coordinates).
  const N = nodeNames.length;
  const cx = 480, cy = 340;
  const R = Math.max(150, N * 34);

  let unboundIol = 0;
  const nodes: LabNode[] = nodeNames.map((nm, i) => {
    const raw = nodesObj[nm] || {};
    const kind = kindOf.get(nm)!;
    const angle = (2 * Math.PI * i) / N - Math.PI / 2;
    const node: LabNode = {
      id: i,
      kind,
      name: nm,
      x: Math.round(N === 1 ? cx : cx + R * Math.cos(angle)),
      y: Math.round(N === 1 ? cy : cy + R * Math.sin(angle)),
    };
    if (kind === "iol") {
      node.ethernet = (maxAdapter.get(nm) ?? 0) + 1;
      unboundIol++;
    }
    if (raw["startup-config"]) {
      warnings.push(`node "${nm}": startup-config "${String(raw["startup-config"])}" skipped (file paths aren't portable — paste the config in the node editor)`);
    }
    return node;
  });
  if (unboundIol > 0) {
    warnings.push(`${unboundIol} IOL node${unboundIol === 1 ? "" : "s"} imported without an image — assign one from your Image library before starting.`);
  }

  const links: LabLink[] = [];
  rawLinks.forEach((l, li) => {
    const eps = (l as Record<string, unknown>)?.endpoints;
    if (!Array.isArray(eps) || eps.length < 2) {
      warnings.push(`link ${li + 1}: skipped (needs 2 endpoints)`);
      return;
    }
    const parsed = eps.map(parseEndpoint);
    if (parsed.some((p) => p === null)) {
      warnings.push(`link ${li + 1}: skipped (unparseable endpoint)`);
      return;
    }
    const unknown = parsed.find((p) => p && !nameToId.has(p.name));
    if (unknown) {
      warnings.push(`link ${li + 1}: skipped (unknown node "${unknown.name}")`);
      return;
    }
    links.push({
      id: li,
      type: parsed.length === 2 ? "p2p" : "segment",
      endpoints: parsed.map((p) => ({
        node: nameToId.get(p!.name)!,
        interface: normIface(kindOf.get(p!.name)!, p!.iface),
      })),
    });
  });

  const doc = emptyLab(String(y.name ?? "Imported lab"));
  doc.nodes = nodes;
  doc.links = links;
  return { doc, warnings };
}

// ---------------------------------------------------------------------------
// Export: iolab LabDocument -> containerlab .clab.yml.
// ---------------------------------------------------------------------------

function clabNodeName(n: LabNode): string {
  // clab node names must be DNS-ish; sanitise iolab display names.
  const s = (n.name || `node${n.id}`).replace(/[^A-Za-z0-9_-]+/g, "-").replace(/^-+|-+$/g, "");
  return s || `node${n.id}`;
}

function clabIface(kind: NodeKind, iface: string): string {
  if (kind === "vpcs") return "eth1"; // clab linux data ifaces start at eth1 (eth0 = mgmt)
  const m = iface.match(/(\d+)\/(\d+)/);
  return m ? `Ethernet${m[1]}/${m[2]}` : "Ethernet0/0";
}

export function exportClab(doc: LabDocument): string {
  const idToNode = new Map(doc.nodes.map((n) => [n.id, n]));
  const nameFor = new Map(doc.nodes.map((n) => [n.id, clabNodeName(n)]));

  const hasIol = doc.nodes.some((n) => n.kind === "iol");
  const hasLinux = doc.nodes.some((n) => n.kind !== "iol");

  let out = `# Generated by iolab from lab "${doc.name}".\n`;
  out += `# NOTE: IOL image refs are placeholders — set them to your containerlab image.\n`;
  out += `name: ${clabNodeName({ id: 0, name: doc.name } as LabNode)}\n\n`;
  out += `topology:\n`;
  if (hasIol || hasLinux) {
    out += `  kinds:\n`;
    if (hasIol) out += `    cisco_iol:\n      image: cisco_iol:17.12.01\n`;
    if (hasLinux) out += `    linux:\n      image: alpine:latest\n`;
  }
  out += `  nodes:\n`;
  for (const n of doc.nodes) {
    const kind = n.kind === "iol" ? "cisco_iol" : "linux";
    out += `    ${nameFor.get(n.id)}:\n      kind: ${kind}\n`;
  }
  if (doc.links.length > 0) {
    out += `  links:\n`;
    for (const l of doc.links) {
      const eps = l.endpoints
        .map((e) => {
          const n = idToNode.get(e.node);
          return `"${nameFor.get(e.node)}:${clabIface(n ? n.kind : "iol", e.interface)}"`;
        })
        .join(", ");
      out += `    - endpoints: [${eps}]\n`;
    }
  }
  return out;
}
