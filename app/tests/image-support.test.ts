import assert from "node:assert/strict";
import { I386_UNSUPPORTED_REASON, imageSupport } from "../src/lib/imageSupport.ts";

const i386 = { id: "i386", filename: "i86bi_m5_unsupported.bin", class: "l3", arch: "i386", sha256: "", size: 2048 };
const x8664 = { ...i386, id: "x8664", filename: "router.bin", arch: "x86_64" };

assert.equal(imageSupport([], i386).supported, false);
assert.equal(imageSupport([], i386).reason, I386_UNSUPPORTED_REASON);
assert.equal(imageSupport(["i386"], i386).supported, true);
assert.equal(imageSupport([], x8664).supported, true);
assert.equal(imageSupport(["i386"], x8664).supported, true);
console.log("image support tests: 5 passed");
