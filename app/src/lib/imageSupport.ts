import type { LibraryImage } from "./labTypes";

/** One reason shared by every GUI control that disables an unsupported image. */
export const I386_UNSUPPORTED_REASON =
  "This supervisor does not advertise i386 IOL support; Apple Silicon Rosetta runs x86_64 IOL images only.";

export interface ImageSupport {
  supported: boolean;
  reason?: string;
}

/** D1: the hello feature is the sole GUI capability signal. */
export function imageSupport(features: readonly string[], image: LibraryImage): ImageSupport {
  if (image.arch !== "i386" || features.includes("i386")) return { supported: true };
  return { supported: false, reason: I386_UNSUPPORTED_REASON };
}
