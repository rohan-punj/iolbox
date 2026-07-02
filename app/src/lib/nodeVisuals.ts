import type { NodeState } from "./labTypes";

export function stateColor(state: NodeState): string {
  switch (state) {
    case "running":
      return "var(--state-running)";
    case "starting":
      return "var(--state-starting)";
    case "crashed":
      return "var(--state-crashed)";
    default:
      return "var(--state-stopped)";
  }
}

export function stateLabel(state: NodeState): string {
  switch (state) {
    case "running":
      return "Running";
    case "starting":
      return "Starting…";
    case "crashed":
      return "Crashed";
    default:
      return "Stopped";
  }
}
