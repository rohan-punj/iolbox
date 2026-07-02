<script lang="ts">
  // Thin wrapper: useSvelteFlow() (used for screen->flow coordinate mapping
  // on palette drop) requires an ancestor SvelteFlowProvider.
  import { SvelteFlowProvider } from "@xyflow/svelte";
  import CanvasInner from "./CanvasInner.svelte";
</script>

<SvelteFlowProvider>
  <CanvasInner />
</SvelteFlowProvider>
