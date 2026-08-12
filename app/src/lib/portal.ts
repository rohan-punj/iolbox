/** Svelte action: moves the element to document.body on mount, removes it on
 *  teardown. Needed for any `position: fixed` overlay that can be rendered
 *  from inside a Svelte Flow node component — nodes live inside two
 *  CSS-transformed ancestors (.svelte-flow__node, .svelte-flow__viewport),
 *  and `position: fixed` is captured by the nearest transformed ancestor
 *  instead of escaping to the real viewport (a standard CSS behavior, not a
 *  Svelte Flow quirk). Without this, a fixed-positioned popover opened from
 *  a node's hover toolbar renders wildly offset from its intended anchor —
 *  found live via MacListPopover appearing far from the node it was opened
 *  for. Popovers rendered outside the node tree (ContextMenu,
 *  InterfacePicker, ChangeImagePopover, IconPicker — all owned by
 *  CanvasInner.svelte directly) don't need this. */
export function portal(node: HTMLElement) {
  document.body.appendChild(node);
  return {
    destroy() {
      node.remove();
    },
  };
}
