<script lang="ts">
  import { labStore } from "./lib/labStore.svelte";
  import TopBar from "./lib/components/TopBar.svelte";
  import Palette from "./lib/components/Palette.svelte";
  import Canvas from "./lib/components/Canvas.svelte";
  import Inspector from "./lib/components/Inspector.svelte";
  import Console from "./lib/components/Console.svelte";
  import Preflight from "./lib/components/Preflight.svelte";
  import ImageManager from "./lib/components/ImageManager.svelte";
  import SplitPane from "./lib/components/SplitPane.svelte";

  let paletteWidth = $state(220);
  let inspectorWidth = $state(300);
  let consoleHeight = $state(240);
</script>

<div class="shell">
  <TopBar />

  <div class="body">
    <SplitPane direction="horizontal" edge="start" bind:size={paletteWidth} min={180} max={360}>
      <Palette />
    </SplitPane>

    <div class="center-col">
      <div class="canvas-area">
        <Canvas />
      </div>
      <SplitPane direction="vertical" edge="end" bind:size={consoleHeight} min={80} max={520}>
        <Console />
      </SplitPane>
    </div>

    <div class="inspector-pane" style:flex-basis={`${inspectorWidth}px`}>
      <Inspector />
    </div>
  </div>
</div>

{#if labStore.showPreflight}
  <Preflight onDismiss={() => (labStore.showPreflight = false)} />
{/if}

{#if labStore.showImageManager}
  <ImageManager onClose={() => (labStore.showImageManager = false)} />
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
