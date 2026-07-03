<script lang="ts">
  // Compact floating style editor for a single annotation (item 6). Anchored
  // near the annotation; controls vary by annotation type:
  //   text / note : text content, color, size (s/m/l), font, fill on/off (note)
  //   rect/ellipse: color, border thickness (1/2/3), fill on/off, fill opacity
  //   line        : color, width (1..4)
  // Every control writes through labStore.updateAnnotation (autosaved). A Delete
  // button removes the annotation. Escape / outside-click closes.
  import { tick } from "svelte";
  import { labStore } from "../labStore.svelte";
  import { ANNO_COLORS } from "../annoTool.svelte";
  import type {
    Annotation,
    AnnotationFont,
    AnnotationSize,
  } from "../labTypes";

  let {
    x,
    y,
    annoId,
    focusText = false,
    onClose,
  }: {
    x: number;
    y: number;
    annoId: string;
    focusText?: boolean;
    onClose: () => void;
  } = $props();

  let el: HTMLDivElement | undefined = $state();
  let textEl: HTMLTextAreaElement | undefined = $state();

  const anno = $derived(labStore.lab.annotations?.find((a) => a.id === annoId));
  const isTextLike = $derived(anno?.type === "text");
  const isShape = $derived(anno?.type === "rect" || anno?.type === "ellipse");
  const isLine = $derived(anno?.type === "line");

  function patch(p: Partial<Annotation>) {
    labStore.updateAnnotation(annoId, p);
  }

  function handleDown(e: PointerEvent) {
    if (el && !el.contains(e.target as Node)) onClose();
  }
  function handleKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }

  $effect(() => {
    if (focusText && isTextLike) {
      void tick().then(() => {
        textEl?.focus();
        textEl?.select();
      });
    }
  });

  // Local text draft so typing feels immediate; committed on every input.
  const sizes: AnnotationSize[] = ["s", "m", "l"];
  const fonts: { id: AnnotationFont; label: string }[] = [
    { id: "sans", label: "Aa" },
    { id: "mono", label: "M" },
    { id: "script", label: "𝒮" },
  ];
</script>

<svelte:window onpointerdown={handleDown} onkeydown={handleKey} />

{#if anno}
  <div class="anno-pop" bind:this={el} style:left={`${x}px`} style:top={`${y}px`} role="dialog">
    <!-- Color swatches (all types). -->
    <div class="row">
      <span class="lbl">Color</span>
      <div class="swatches">
        {#each ANNO_COLORS as c (c)}
          <button
            class="dot"
            class:on={(anno.color ?? ANNO_COLORS[0]) === c}
            style:background={c}
            aria-label="Color"
            onclick={() => patch({ color: c })}
          ></button>
        {/each}
      </div>
    </div>

    {#if isTextLike}
      <div class="row">
        <span class="lbl">Text</span>
        <textarea
          bind:this={textEl}
          class="text-field nodrag"
          rows="2"
          spellcheck="false"
          value={(anno as any).text ?? ""}
          oninput={(e) => patch({ text: (e.currentTarget as HTMLTextAreaElement).value })}
        ></textarea>
      </div>
      <div class="row">
        <span class="lbl">Size</span>
        <div class="seg">
          {#each sizes as s (s)}
            <button
              class="seg-btn"
              class:on={((anno as any).size ?? "m") === s}
              onclick={() => patch({ size: s })}
            >{s.toUpperCase()}</button>
          {/each}
        </div>
      </div>
      <div class="row">
        <span class="lbl">Font</span>
        <div class="seg">
          {#each fonts as f (f.id)}
            <button
              class="seg-btn"
              class:on={((anno as any).font ?? "sans") === f.id}
              data-font={f.id}
              onclick={() => patch({ font: f.id })}
            >{f.label}</button>
          {/each}
        </div>
      </div>
      <div class="row">
        <span class="lbl">Fill</span>
        <button
          class="toggle"
          class:on={(anno as any).fill === true}
          onclick={() => patch({ fill: !((anno as any).fill === true) })}
        >{(anno as any).fill ? "On" : "Off"}</button>
      </div>
    {/if}

    {#if isShape}
      <div class="row">
        <span class="lbl">Border</span>
        <div class="seg">
          {#each [1, 2, 3] as b (b)}
            <button
              class="seg-btn"
              class:on={Math.round((anno as any).border ?? 2.5) === b || (b === 2 && (anno as any).border === undefined)}
              onclick={() => patch({ border: b })}
            >{b}px</button>
          {/each}
        </div>
      </div>
      <div class="row">
        <span class="lbl">Opacity</span>
        <input
          class="slider nodrag"
          type="range"
          min="0"
          max="0.6"
          step="0.02"
          value={(anno as any).fillOpacity ?? 0.12}
          oninput={(e) => patch({ fillOpacity: Number((e.currentTarget as HTMLInputElement).value) })}
        />
      </div>
    {/if}

    {#if isLine}
      <div class="row">
        <span class="lbl">Width</span>
        <div class="seg">
          {#each [1, 2, 3, 4] as w (w)}
            <button
              class="seg-btn"
              class:on={Math.round((anno as any).width ?? 2.5) === w || (w === 2 && (anno as any).width === undefined)}
              onclick={() => patch({ width: w })}
            >{w}</button>
          {/each}
        </div>
      </div>
    {/if}

    <div class="foot">
      <button class="del" onclick={() => { labStore.removeAnnotation(annoId); onClose(); }}>Delete</button>
    </div>
  </div>
{/if}

<style>
  .anno-pop {
    position: fixed;
    z-index: 1000;
    width: 240px;
    background: var(--bg-elevated);
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-md);
    box-shadow: var(--shadow-lg);
    padding: 8px;
    display: flex;
    flex-direction: column;
    gap: 7px;
    font-size: var(--fs-xs);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .lbl {
    width: 52px;
    flex-shrink: 0;
    color: var(--ink-2);
    font-size: var(--fs-xs);
  }
  .swatches {
    display: flex;
    gap: 5px;
    flex-wrap: wrap;
  }
  .dot {
    all: unset;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    cursor: pointer;
    box-shadow: 0 0 0 1px var(--border-strong);
    border: 2px solid transparent;
    box-sizing: border-box;
  }
  .dot.on {
    box-shadow: 0 0 0 2px var(--accent);
  }
  .text-field {
    flex: 1;
    min-width: 0;
    resize: none;
    font-family: var(--font-ui);
    font-size: var(--fs-sm);
  }
  .seg {
    display: flex;
    gap: 3px;
  }
  .seg-btn {
    all: unset;
    box-sizing: border-box;
    padding: 3px 7px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    color: var(--ink-2);
    cursor: pointer;
    font-size: 11px;
    text-align: center;
  }
  .seg-btn[data-font="mono"] {
    font-family: var(--font-mono);
  }
  .seg-btn[data-font="script"] {
    font-family: "Segoe Script", "Comic Sans MS", cursive;
  }
  .seg-btn:hover {
    background: var(--bg-hover);
    color: var(--ink);
  }
  .seg-btn.on {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-muted);
  }
  .toggle {
    all: unset;
    box-sizing: border-box;
    padding: 3px 12px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-strong);
    color: var(--ink-2);
    cursor: pointer;
    font-size: 11px;
  }
  .toggle.on {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-muted);
  }
  .slider {
    flex: 1;
    padding: 0;
    accent-color: var(--accent);
  }
  .foot {
    display: flex;
    justify-content: flex-end;
    border-top: 1px solid var(--border);
    padding-top: 6px;
  }
  .del {
    all: unset;
    box-sizing: border-box;
    padding: 4px 12px;
    border-radius: var(--radius-sm);
    color: var(--danger);
    cursor: pointer;
    font-size: var(--fs-xs);
    font-weight: 600;
  }
  .del:hover {
    background: var(--state-crashed-bg);
  }
</style>
