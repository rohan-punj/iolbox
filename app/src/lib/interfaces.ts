// Interface enumeration shared by the link-add Interface Picker (R2.1) and the
// Node Edit dialog (R2.2). An IOL node exposes `ethernet` + `serial` adapter
// groups, each group = 4 ports; VPCS collapses to a single `eth0`.

import type { LabNode } from "./labTypes";
import { labStore } from "./labStore.svelte";

/** All interface ids a node *could* expose given its adapter counts. */
export function allInterfaces(node: LabNode): string[] {
  // VPCS + the single-interface builtin NAT gateway all expose exactly one
  // "eth0" port.
  if (node.kind === "vpcs" || node.kind === "nat") return ["eth0"];
	if (node.kind === "tool" || node.kind === "pc") return ["eth1"];
  const list: string[] = [];
  const eth = Math.max(node.ethernet ?? 1, 0);
  const ser = Math.max(node.serial ?? 0, 0);
  for (let a = 0; a < Math.max(eth, 1); a++) {
    for (let p = 0; p < 4; p++) list.push(`e${a}/${p}`);
  }
  for (let a = 0; a < ser; a++) {
    for (let p = 0; p < 4; p++) list.push(`s${a}/${p}`);
  }
  return list;
}

/** Interface ids currently consumed by a link on this node. */
export function usedInterfaces(nodeId: number): Set<string> {
  const used = new Set<string>();
  for (const l of labStore.lab.links) {
    for (const e of l.endpoints) {
      if (e.node === nodeId) used.add(e.interface);
    }
  }
  return used;
}

/** Free (unwired) interfaces on a node. */
export function freeInterfaces(node: LabNode): string[] {
  const used = usedInterfaces(node.id);
  return allInterfaces(node).filter((i) => !used.has(i));
}

/** Next free interface, or the first interface as a last resort. */
export function nextFreeInterface(node: LabNode): string {
  const free = freeInterfaces(node);
  if (free.length) return free[0];
  if (node.kind === "iol") return "e0/0";
	return node.kind === "tool" || node.kind === "pc" ? "eth1" : "eth0";
}
