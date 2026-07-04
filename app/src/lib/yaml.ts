// iolab's native lab format is YAML — far more readable than JSON, especially
// for multi-line startup-configs (rendered as literal block scalars). This
// module is the single YAML boundary: the durable store, export/import, and the
// launcher folder all carry lab documents as YAML text; only the runtime
// lab.load wire keeps a JSON object (the supervisor executes from it).
//
// Uses js-yaml for robust parse/dump (correct quoting + block scalars). Reading
// falls back to JSON so labs saved before the YAML switch still open.

import { dump, load } from "js-yaml";
import type { LabDocument } from "./labTypes";

/** Serialise a lab document to YAML text (block scalars for multi-line configs,
 *  no line wrapping so configs stay verbatim). */
export function labToYaml(doc: LabDocument): string {
  return dump(doc, {
    lineWidth: -1, // never wrap — keep startup-configs byte-for-byte readable
    noRefs: true, // no anchors/aliases — plain, diffable output
  });
}

/** Parse a lab document from text. Accepts YAML (the native format) or JSON
 *  (labs saved before the switch). Throws if the text isn't a lab-shaped object. */
export function labFromText(text: string): LabDocument {
  const trimmed = text.trimStart();
  const parsed = trimmed.startsWith("{") ? JSON.parse(text) : load(text);
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("not a lab document");
  }
  const doc = parsed as LabDocument;
  if (!Array.isArray(doc.nodes) || !Array.isArray(doc.links)) {
    throw new Error("not a lab document (missing nodes/links)");
  }
  return doc;
}
