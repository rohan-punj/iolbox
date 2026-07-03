<script lang="ts">
  import { labStore } from "./lib/labStore.svelte";
  import { consoleUiStore } from "./lib/consoleUiStore.svelte";
  import TopBar from "./lib/components/TopBar.svelte";
  import Palette from "./lib/components/Palette.svelte";
  import Canvas from "./lib/components/Canvas.svelte";
  import Inspector from "./lib/components/Inspector.svelte";
  import TasksPane from "./lib/components/TasksPane.svelte";
  import Console from "./lib/components/Console.svelte";
  import Preflight from "./lib/components/Preflight.svelte";
  import ImageManager from "./lib/components/ImageManager.svelte";
  import LabBrowser from "./lib/components/LabBrowser.svelte";
  import SplitPane from "./lib/components/SplitPane.svelte";

  let paletteWidth = $state(220);
  let inspectorWidth = $state(300);
  let tasksWidth = $state(360);
  let consoleHeight = $state(240);
  let consoleWidth = $state(420);

  // Only show the console dock when at least one tab is open — an empty dock
  // just steals canvas space. Placement (bottom bar vs right pane) is the
  // persisted user choice from consoleUiStore.
  const showConsole = $derived(
    labStore.openConsoleTabs.length > 0 || labStore.openCaptureTabs.length > 0
  );
  const dockRight = $derived(consoleUiStore.dockSide === "right");

  // Right pane. The Tasks pane (TopBar toggle) takes precedence over the
  // empty-selection auto-hide and, while toggled on, wins even when a node is
  // selected. Otherwise the Inspector shows only when something is selected;
  // clicking empty canvas clears selection (CanvasInner onPaneClick), collapsing
  // the pane and handing its width back to the canvas.
  const showTasks = $derived(labStore.showTasks);
  const showInspector = $derived(
    !showTasks && (labStore.selectedNodeId !== null || labStore.selectedLinkId !== null)
  );
</script>

<div class="shell">
  <TopBar />

  <div class="body">
    <SplitPane
      direction="horizontal"
      edge="start"
      bind:size={paletteWidth}
      min={180}
      max={360}
      storageKey="iolab.split.palette"
    >
      <Palette />
    </SplitPane>

    <div class="center-col">
      <div class="canvas-area">
        <Canvas />
      </div>
      {#if showConsole && !dockRight}
        <SplitPane
          direction="vertical"
          edge="end"
          bind:size={consoleHeight}
          min={80}
          max={520}
          storageKey="iolab.split.consoleBottom"
        >
          <Console />
        </SplitPane>
      {/if}
    </div>

    {#if showTasks}
      <div class="inspector-pane" style:flex-basis={`${tasksWidth}px`}>
        <TasksPane />
      </div>
    {:else if showInspector}
      <div class="inspector-pane" style:flex-basis={`${inspectorWidth}px`}>
        <Inspector />
      </div>
    {/if}

    {#if showConsole && dockRight}
      <SplitPane
        direction="horizontal"
        edge="end"
        bind:size={consoleWidth}
        min={280}
        max={720}
        storageKey="iolab.split.consoleRight"
      >
        <Console />
      </SplitPane>
    {/if}
  </div>
</div>

{#if labStore.showPreflight}
  <Preflight onDismiss={() => (labStore.showPreflight = false)} />
{/if}

{#if labStore.showImageManager}
  <ImageManager onClose={() => (labStore.showImageManager = false)} />
{/if}

{#if labStore.showLabBrowser}
  <LabBrowser onClose={() => (labStore.showLabBrowser = false)} />
{/if}

<style>
  .shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
    width: 100vw;
  }
  .body {
    flex: 1;
    display: flex;
    min-height: 0;
  }
  .center-col {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .canvas-area {
    flex: 1;
    min-height: 0;
  }
  .inspector-pane {
    flex-shrink: 0;
    background: var(--bg-1);
    border-left: 1px solid var(--border);
    overflow: hidden;
  }
</style>
