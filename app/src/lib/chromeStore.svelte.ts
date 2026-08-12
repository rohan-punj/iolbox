import { untrack } from "svelte";
import { annoTool } from "./annoTool.svelte";
import { labStore } from "./labStore.svelte";

const STORAGE_KEY = "iolbox.chrome.autohide";
const HIDE_AFTER_MS = 2000;
const EDGE_PX = 8;

function readEnabled(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

function isChromeTarget(target: EventTarget | null): boolean {
  return typeof Element !== "undefined" && target instanceof Element &&
    target.closest("[data-chrome-surface], [role=menu], [role=dialog]:not(.float-win)") !== null;
}

class ChromeStore {
  hidden = $state(false);
  enabled = $state(false);
  private holds = $state(0);
  private now = $state(Date.now());
  private lastActivity = $state(Date.now());
  private focusEpoch = $state(0);
  private pointerEpoch = $state(0);
  private clockTimer: ReturnType<typeof setInterval> | null = null;

  idle = $derived(this.now - this.lastActivity);
  suppressed = $derived.by(() => {
    // Open-menu holds come from ContextMenu, AnnoStylePopover,
    // ChangeImagePopover, IconPicker, and InterfacePicker; active-drag holds
    // come from SplitPane/dragMove, plus annoTool and xyflow's node-drag class.
    void this.focusEpoch;
    void this.pointerEpoch;
    const held = this.holds > 0;
    const openMenu = held;
    const activeDrag = annoTool.active !== null || held || this.nodeIsDragging();
    const focusedControl = this.focusedChromeControl();
    const modal =
      labStore.showPreflight ||
      labStore.showImageManager ||
      labStore.showLabBrowser ||
      labStore.pendingSwitch !== null;
    const error = labStore.lastError !== null || labStore.providerStatus === "error";
    return openMenu || activeDrag || focusedControl || modal || error;
  });
  shouldHide = $derived(
    this.enabled && labStore.labRunning && this.idle > HIDE_AFTER_MS && !this.suppressed
  );

  constructor() {
    this.enabled = readEnabled();
  }

  start(): (() => void) | undefined {
    if (typeof window === "undefined" || this.clockTimer) return;
    this.clockTimer = setInterval(() => (this.now = Date.now()), 100);
    return () => {
      if (this.clockTimer) clearInterval(this.clockTimer);
      this.clockTimer = null;
    };
  }

  setEnabled(value: boolean) {
    this.enabled = value;
    try {
      localStorage.setItem(STORAGE_KEY, String(value));
    } catch {
      /* localStorage may be unavailable (private mode) */
    }
    this.reveal();
  }

  toggleEnabled() {
    this.setEnabled(!this.enabled);
  }

  syncVisibility() {
    this.hidden = this.shouldHide;
  }

  reveal() {
    const stamp = Date.now();
    this.lastActivity = stamp;
    this.now = stamp;
    this.hidden = false;
  }

  // Reveals ONLY on proximity to the left edge (icon rail) or top edge (top
  // bar) — the chrome surfaces that live there. This used to ALSO reveal on
  // any pointer movement anywhere on screen: the unconditional
  // setTimeout(reveal, POINTER_DEBOUNCE_MS) below fired ~250ms after every
  // single move regardless of position, so auto-hide effectively never held
  // — any mouse activity brought the chrome back. Found live ("hide chrome
  // works but appears any time mouse cursor appears").
  onPointerMove(event: PointerEvent) {
    this.pointerEpoch += 1;
    if (event.clientX <= EDGE_PX || event.clientY <= EDGE_PX) this.reveal();
  }

  onPointerUp() {
    this.pointerEpoch += 1;
  }

  onKeyDown(event: KeyboardEvent) {
    if (event.key === "Alt") this.reveal();
  }

  onFocusIn(event: FocusEvent) {
    this.focusEpoch += 1;
    if (isChromeTarget(event.target)) this.reveal();
  }

  // untrack() around the read-modify-write is load-bearing: callers invoke
  // this from inside $effect(() => chromeStore.hold()) (ContextMenu,
  // AnnoStylePopover, ChangeImagePopover, IconPicker, InterfacePicker,
  // SplitPane, dragMove). Without untrack, `this.holds += 1` both reads
  // and writes the same $state field DURING that effect's own run, which
  // makes the effect depend on `holds` and then immediately re-triggers
  // itself from its own write — Svelte trips effect_update_depth_exceeded
  // (reproduced live: opening the overflow menu wedged the whole app,
  // silently breaking outside-click dismiss and everything downstream).
  hold(): () => void {
    untrack(() => {
      this.holds += 1;
    });
    let released = false;
    return () => {
      if (released) return;
      released = true;
      untrack(() => {
        this.holds = Math.max(0, this.holds - 1);
        if (this.holds === 0) this.reveal();
      });
    };
  }

  private focusedChromeControl(): boolean {
    if (typeof document === "undefined") return false;
    const active = document.activeElement;
    return !!active && active.matches(":focus-visible") && isChromeTarget(active);
  }

  private nodeIsDragging(): boolean {
    return typeof document !== "undefined" &&
      document.querySelector(".svelte-flow__node.dragging") !== null;
  }
}

export const chromeStore = new ChromeStore();
