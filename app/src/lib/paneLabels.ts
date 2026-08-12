import { labStore } from "./labStore.svelte";

export function nodeName(nodeId: number): string {
  return labStore.lab.nodes.find((node) => node.id === nodeId)?.name ?? `#${nodeId}`;
}

/** Live-capture title: `R1 e0/0 ⇄ e0/0 SW1`. */
export function captureTitle(linkId: number): string {
  const link = labStore.lab.links.find((item) => item.id === linkId);
  if (!link) return `capture #${linkId}`;
  const [a, b] = link.endpoints;
  const an = nodeName(a?.node ?? -1);
  const bn = nodeName(b?.node ?? -1);
  return `${an} ${a?.interface ?? ""} ⇄ ${b?.interface ?? ""} ${bn}`;
}
