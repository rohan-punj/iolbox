<script lang="ts">
  import { labStore } from "../labStore.svelte";
  import { uiSvg } from "../icons.svelte";

  const text = $derived(labStore.lab.tasks ?? "");
  let editing = $state(false);
  let draft = $state("");

  // Parse the task text into render rows. Markdown-lite:
  //   "- [ ] foo" / "- [x] foo" → live checkbox (carries its source line index)
  //   "## Heading"              → heading
  //   everything else            → plain paragraph (blank lines = spacer)
  type Row =
    | { kind: "check"; line: number; done: boolean; label: string }
    | { kind: "heading"; text: string }
    | { kind: "text"; text: string }
    | { kind: "blank" };

  const rows = $derived.by<Row[]>(() => {
    const lines = text.split("\n");
    return lines.map((raw, i): Row => {
      const m = /^\s*-\s*\[([ xX])\]\s?(.*)$/.exec(raw);
      if (m) return { kind: "check", line: i, done: m[1].toLowerCase() === "x", label: m[2] };
      const h = /^\s*##\s+(.*)$/.exec(raw);
      if (h) return { kind: "heading", text: h[1] };
      if (raw.trim() === "") return { kind: "blank" };
      return { kind: "text", text: raw };
    });
  });

  // Toggle one checkbox: flip the [ ]/[x] marker on its source line and write
  // the whole text back through the autosave path. Preserves all other lines.
  function toggle(lineIdx: number) {
    const lines = text.split("\n");
    const raw = lines[lineIdx];
    if (raw === undefined) return;
    lines[lineIdx] = raw.replace(/^(\s*-\s*\[)([ xX])(\])/, (_all, pre, mark, post) => {
      const next = mark.toLowerCase() === "x" ? " " : "x";
      return `${pre}${next}${post}`;
    });
    labStore.setTasks(lines.join("\n"));
  }

  function startEdit() {
    draft = text;
    editing = true;
  }
  function saveEdit() {
    labStore.setTasks(draft);
    editing = false;
  }
  function cancelEdit() {
    editing = false;
  }
</script>

<div class="tasks">
  <div class="tasks-head">
    <span class="title">{@html uiSvg("tasks", 15)} Tasks</span>
    <div class="head-actions">
      {#if editing}
        <button class="btn btn-ghost sm" onclick={cancelEdit}>Cancel</button>
        <button class="btn btn-primary sm" onclick={saveEdit}>Save</button>
      {:else}
        <button class="btn btn-ghost sm" onclick={startEdit}>{@html uiSvg("edit", 13)} Edit</button>
      {/if}
      <button
        class="btn btn-ghost close-btn"
        title="Close tasks"
        aria-label="Close tasks"
        onclick={() => (labStore.showTasks = false)}
      >{@html uiSvg("x", 14)}</button>
    </div>
  </div>

  <div class="tasks-body">
    {#if editing}
      <textarea
        class="tasks-editor mono"
        spellcheck="false"
        bind:value={draft}
        placeholder="## Objectives&#10;- [ ] First task&#10;- [ ] Second task"
      ></textarea>
    {:else if text.trim() === ""}
      <div class="empty">No tasks yet — click Edit to add instructions for this lab.</div>
    {:else}
      <div class="reader">
        {#each rows as row, i (i)}
          {#if row.kind === "check"}
            <label class="check-row" class:done={row.done}>
              <input type="checkbox" checked={row.done} onchange={() => toggle(row.line)} />
              <span class="check-label">{row.label}</span>
            </label>
          {:else if row.kind === "heading"}
            <h3 class="reader-h">{row.text}</h3>
          {:else if row.kind === "blank"}
            <div class="reader-gap"></div>
          {:else}
            <p class="reader-p">{row.text}</p>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
</div>

<style>
  .tasks {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }
  .tasks-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    padding: var(--sp-3);
    border-bottom: 1px solid var(--border);
    flex-shrink: 0;
  }
  .title {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    font-size: var(--fs-md);
    font-weight: 600;
    color: var(--ink);
  }
  .title :global(svg) {
    color: var(--accent);
  }
  .head-actions {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .btn.sm {
    padding: 4px 10px;
    font-size: var(--fs-xs);
  }
  .btn.sm :global(svg) {
    width: 13px;
    height: 13px;
  }
  .close-btn {
    padding: 5px;
    width: 26px;
    height: 26px;
    justify-content: center;
  }
  .tasks-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--sp-3);
  }
  .empty {
    font-size: var(--fs-sm);
    color: var(--ink-3);
    line-height: 1.5;
  }
  .tasks-editor {
    width: 100%;
    height: 100%;
    min-height: 240px;
    resize: none;
    line-height: 1.55;
    padding: var(--sp-2);
  }
  .reader {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .reader-h {
    margin: var(--sp-3) 0 2px;
    font-size: var(--fs-md);
    font-weight: 650;
    color: var(--ink);
  }
  .reader-h:first-child {
    margin-top: 0;
  }
  .reader-p {
    margin: 0;
    font-size: var(--fs-sm);
    line-height: 1.55;
    color: var(--ink-2);
  }
  .reader-gap {
    height: var(--sp-2);
  }
  .check-row {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: 3px 4px;
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-size: var(--fs-sm);
    line-height: 1.5;
    color: var(--ink);
  }
  .check-row:hover {
    background: var(--bg-hover);
  }
  .check-row input {
    margin-top: 2px;
    width: 15px;
    height: 15px;
    accent-color: var(--accent);
    flex-shrink: 0;
    cursor: pointer;
  }
  .check-row.done .check-label {
    color: var(--ink-3);
    text-decoration: line-through;
  }
  .check-label {
    min-width: 0;
  }
</style>
