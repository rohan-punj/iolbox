// Persistent view preference for learned IOL MAC disclosure. The supervisor's
// dirstat learner is always active; this preference only controls whether the
// GUI asks the node.macs handler to include its attribution results.

const LEARN_IOL_KEY = "iolbox.mac.learnIol";

function initialLearnIol(): boolean {
  try {
    return localStorage.getItem(LEARN_IOL_KEY) === "1";
  } catch {
    // localStorage may be unavailable (private mode / SSR).
    return false;
  }
}

class MacUiStore {
  learnIol = $state(false);

  constructor() {
    this.learnIol = initialLearnIol();
  }

  setLearnIol(on: boolean) {
    this.learnIol = on;
    try {
      localStorage.setItem(LEARN_IOL_KEY, on ? "1" : "0");
    } catch {
      // Ignore persistence failures; the current session still reflects the choice.
    }
  }

  toggleLearnIol() {
    this.setLearnIol(!this.learnIol);
  }
}

export const macUiStore = new MacUiStore();
