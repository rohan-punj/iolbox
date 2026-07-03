<script lang="ts">
  import { type NodeProps } from "@xyflow/svelte";
  import { tick } from "svelte";
  import { labStore } from "../labStore.svelte";
  import { annoTool } from "../annoTool.svelte";
  import type { AnnotationFont, AnnotationSize } from "../labTypes";

  let { id, data, selected }: NodeProps = $props();

  const annoId = $derived((data as any).annoId as string);
  const text = $derived((data as any).text as string);
  const size = $derived(((data as any).size as AnnotationSize) ?? "m");
  const color = $derived((data as any).color as string | undefined);
  const font = $derived(((data as any).font as AnnotationFont) ?? "sans");
  const fill = $derived(((data as any).fill as boolean) ?? false);

  let editing = $state(false);
  let draft = $state("");
  let inputEl: HTMLTextAreaElement | undefined = $state();

  // Begin inline editing when CanvasInner requests it (context-menu "Edit text"
  // or right after placement). Double-click routes to the style popover instead.
  $effect(() => {
    if (annoTool.editRequestId === annoId) {
      annoTool.editRequestId = null;
      beginEdit();
    }
  });

  async function beginEdit() {
    draft = text;
    editing = true;
    await tick();
    inputEl?.focus();
    inputEl?.select();
  }

  function commit() {
    if (!editing) return;
    editing = false;
    const next = draft.trim();
    if (next === "") {
      // Empty text annotations are meaningless — drop them.
      labStore.removeAnnotation(annoId);
      return;
    }
    if (next !== text) labStore.updateAnnotation(annoId, { text: next });
  }

  function onKeydown(e: KeyboardEvent) {
    // Escape commits (blur), Enter commits too (Shift+Enter = newline).
    if (e.key === "Escape") {
      e.preventDefault();
      inputEl?.blur();
    } else if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      inputEl?.blur();
    }
  }

  // Double-click opens the compact style popover with the text field focused,
  // so quick inline text editing still works from a single dblclick.
  function onDblClick(e: MouseEvent) {
    e.stopPropagation();
    annoTool.requestStyle?.(annoId, e.clientX, e.clientY, true);
  }

  const fontFamily = $derived(
    font === "mono"
      ? "var(--font-mono)"
      : font === "script"
        ? "'Segoe Script', 'Comic Sans MS', cursive"
        : "var(--font-ui)"
  );
</script>

<div
  class="anno-text"
  class:selected
  class:editing
  class:filled={fill && !editing}
  data-size={size}
  style:color={color ?? "var(--ink)"}
  style:font-family={fontFamily}
  style:--note-fill={color ?? "var(--accent)"}
  ondblclick={onDblClick}
  role="button"
  tabindex="-1"
>
  {#if editing}
    <textarea
      bind:this={inputEl}
      bind:value={draft}
      class="anno-input nodrag"
      style:font-family={fontFamily}
      spellcheck="false"
      rows="1"
      onblur={commit}
      onkeydown={onKeydown}
    ></textarea>
  {:else}
    <span class="anno-label">{text}</span>
  {/if}
</div>

<style>
  .anno-text {
    max-width: 320px;
    padding: 2px 4px;
    line-height: 1.35;
    font-family: var(--font-ui);
    font-weight: 550;
    white-space: pre-wrap;
    cursor: default;
    border-radius: var(--radius-sm);
    border: 1px solid transparent;
  }
  /* Note = text with a rounded, semi-transparent fill of the chosen color. */
  .anno-text.filled {
    padding: 6px 10px;
    background: color-mix(in oklab, var(--note-fill) 20%, transparent);
    border-color: color-mix(in oklab, var(--note-fill) 45%, transparent);
    border-radius: var(--radius-md);
  }
  .anno-text[data-size="s"] {
    font-size: var(--fs-sm);
  }
  .anno-text[data-size="m"] {
    font-size: var(--fs-lg);
  }
  .anno-text[data-size="l"] {
    font-size: 26px;
  }
  .anno-text.selected {
    border-color: var(--accent);
    box-shadow: 0 0 0 2px color-mix(in oklab, var(--accent) 24%, transparent);
  }
  .anno-label {
    display: block;
  }
  .anno-input {
    all: unset;
    box-sizing: border-box;
    display: block;
    width: 260px;
    min-height: 1.35em;
    color: inherit;
    font: inherit;
    line-height: 1.35;
    background: var(--panel-solid);
    border: 1px solid var(--accent);
    border-radius: var(--radius-sm);
    padding: 2px 4px;
    resize: none;
    overflow: hidden;
  }
</style>
