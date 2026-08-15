You are running an ADVERSARIAL REVIEW pass (sol, medium reasoning) over the M5
implementation plan for the Apple Silicon macOS track of iolbox, in the worktree
`J:\Claude code\iolab-m5-wt` on branch `luna/macos-m5-honest-caps`.

Read, in this order:

1. `docs/macos-m5-plan.md` — the plan under review (written by a prior sol pass).
2. `docs/macos-m5-prompt.md` — the immutable scope/acceptance statement the plan
   must satisfy.
3. `docs/macos-m4-handoff.md`, `docs/macos-m4-result.md`,
   `docs/macos-m3-handoff.md`, `docs/macos-m1-handoff.md` — carried-forward
   gotchas and the honesty bar.
4. `docs/macos-arm64-plan.md` §M5 (immutable).

**Do not implement anything. Do not edit any file except the one deliverable
below. No git operations.**

Deliverable: `J:\Claude code\iolab-m5-wt\docs\macos-m5-plan-review.md`.

## Your job

Find where the plan is wrong, overreaching, under-specified, or would fail on
real hardware. Assume the plan is confidently written and that confidence is
not evidence. M2, M3 and M4 each found 6-11 real defects only when code met
real hardware; a plan that reads cleanly is exactly the state those sessions
were in before hardware embarrassed them.

Verify every factual claim the plan makes about the codebase by reading the
actual file. The plan asserts many specific line numbers, field names, file
paths and behaviors. Several have already been spot-checked and are correct
(`CatalogEntry.disabled` exists as an optional string at
`app/src/lib/nodeCatalog.ts:12`; `/var/lib/iolbox/macos-canary.json` is really
written by `packaging/macos/guest/30-canary.sh:214`). **Check the rest
yourself** and list any that are wrong — a plan citing a symbol that does not
exist will send the implementer down a dead end.

## Specific questions you must answer, with evidence

1. **Scope vs. estimate.** The milestone is estimated at 0.5-1 focused day.
   Plan §4 modifies ten GUI files and §9 adds five test suites. Is the GUI
   surface list actually necessary to satisfy "the GUI never offers i386 IOL
   images as supported on an Apple Silicon target", or is some of it gold
   plating? Identify the minimum sufficient set and the genuinely optional
   remainder, and say which surfaces would leave a real hole if skipped. Be
   concrete about which of `App.svelte`, `CanvasInner.svelte`,
   `NodeActions.svelte`, `Inspector.svelte`, `ChangeImagePopover.svelte`,
   `ImageManager.svelte` are load-bearing for the acceptance criterion.

2. **The server-side `image_arch_mismatch` rejection (§3).** Is adding this
   within M5's stated scope ("a new explicit i386-disable capability", additive,
   do not alter the `arch` field's meaning), or is it a behavior change beyond
   it? Check that `image_arch_mismatch` is genuinely already defined and what
   currently emits it. Determine what happens to a *running* lab, an autosaved
   doc, and `lab.load` when a node references an i386 image under
   `DisableI386` — the plan claims load stays fine and only start rejects;
   verify that against the actual load/start code paths rather than accepting
   it. Flag any path where this turns a previously-working non-Mac flow into an
   error (blast radius).

3. **Fail-open vs the acceptance criteria.** The plan chooses fail-open
   (absent signal ⇒ advertise i386) and adds three drift detectors. Walk the
   actual upgrade path in `packaging/macos/guest/40-install-payload.sh` and
   `runtime/files/native/install.sh` and determine whether a Mac can in
   practice end up running with the canary drop-in present but the
   `Environment=` line absent — e.g. an older drop-in written by an M1-M4-era
   provisioner that the M5 provisioner never rewrites because the machine is
   only `start`ed, not `upgrade`d. If that hole exists, say exactly which code
   path leaves it open and what the minimal fix is. This is the single most
   likely way the shipped product silently keeps lying.

4. **`iolArchitectures` — necessary or speculative?** The acceptance criteria
   only require that hello *omits* i386 and that the GUI never offers it. The
   plan adds a new positive hello field. Argue both sides and give a verdict:
   is the extra field justified by the "old supervisor vs. genuinely
   unsupported" ambiguity the plan cites, or is it an unnecessary protocol
   addition in a milestone whose own instruction is "the smallest change that
   satisfies all five acceptance criteria"? If you keep it, confirm the
   camelCase name matches this protocol's existing convention (check
   `egressNote` and the rest of `protocol.HelloResult` and
   `app/src/lib/protocol.ts`) and that `omitempty` genuinely produces
   byte-identical hello JSON for non-Mac targets.

5. **Diagnostics derivation (§6).** The plan makes `execution=rosetta-amd64`
   conditional on four predicates including "the supervisor service/hello is
   reachable". Is service reachability actually a sound input to a statement
   about *execution mode*? A guest could be genuinely Rosetta-translated with a
   crashed supervisor. Critique the predicate set and propose a corrected one.
   Also check whether `structural_canary` sourcing from
   `/var/lib/iolbox/macos-canary.json` matches what `30-canary.sh` actually
   writes (field names, verdict values, timestamp format) — quote the real
   record shape.

6. **Hardware sequence realism (§11).** For each of the 7 harness steps, say
   whether it can actually run given what the M4 handoff and M1 handoff say
   about this Mac and this Lima machine. Specifically:
   - Does `hardware-m1.sh --phase install-image-and-lab` exist with that exact
     phase name and those exact flags? Check the script.
   - Is the x86_64 image path in the plan's command line the one actually on
     the Mac? (M4's result names
     `x86_64_crb_linux_l2-adventerprisek9-ms.iol17.18.02.bin` as the *switch*
     image; the plan cites a router path. Verify what the harnesses expect and
     flag the mismatch if there is one — do not assume either is right.)
   - The plan's step 5 requires a real browser against the loopback GUI over an
     SSH tunnel. Is the GUI reachable that way given M3's finding that the WS
     bridge requires a session cookie and same-origin `Origin` header on every
     route? Will a plain browser session over `ssh -L` actually work, or does
     the origin/port rewrite break it? This is a concrete M3 defect that could
     invalidate the entire GUI proof; reason it through against `wsbridge.go`.
   - The plan reuses machine `iolbox-m4-e2e`. Assess the risk that doing so
     destroys M4 evidence or state a future session needs, versus creating a
     fresh `iolbox-m5-e2e`. Recommend one.

7. **The payload splice (§10).** The owner has already executed a dry run of
   this and it worked: build `supervisor-linux-amd64` via `build-release.sh` on
   Windows (Node 26.7 + Go 1.26.4 are present), scp it to the Mac, copy the
   extracted `iolbox-server-v0.5.3-netprobe-cgroupfd-fix` tree, replace
   `bin/supervisor` (mode 0755), and retar. The resulting probe tarball is
   `~/iolbox-m0/iolbox-server-v0.5.4-m5-probe.tar.gz`, sha256
   `804aab32718d8f072bc2c38e135a6ec18a83e79c011a2df25c56754e0d0da022`. Note
   also that `runtime/pack-native.sh` **cannot** run on the Windows host: the
   `J:` drive does not honor `chmod`, so its `-x` preflight on the vpcs binary
   always fails. Given that, correct §10 where it is now wrong or
   over-cautious, and answer: does the splice need to happen *in the Linux
   guest* as the plan says, or is doing it on macOS acceptable — specifically,
   does BSD `tar` on macOS produce a tarball that the payload's `install.sh`
   unpacks correctly with the right modes? Say what would prove it.

8. **Anything the plan omits entirely.** In particular check:
   `docs/providers.md` and `docker/README.md` both mention i386 — do they need
   updating for honesty? Does the launcher's own `README.md` exist? Is there
   any *existing* test that will now fail because the hello feature list
   changed shape (search the whole repo for tests asserting on `features`)?
   Does `app/src/lib/mockTransport.ts` feed any test that would break?

## Output format

Write `docs/macos-m5-plan-review.md` with:

- A one-paragraph verdict: is the plan safe to implement as written, safe with
  the listed corrections, or does it need a rewrite of a specific section?
- A numbered findings table: `#`, severity (BLOCKER / MAJOR / MINOR / NOTE),
  the claim or decision at fault, the evidence (file:line), and the specific
  correction.
- An explicit "corrections to fold into the plan" list the implementer can
  apply mechanically.
- A "questions only the owner can answer" list — keep it short and only
  include things genuinely undecidable from the repo.

Be specific and cite `file:line`. A finding without evidence is noise. If a
section of the plan is correct and well-reasoned, say so briefly rather than
manufacturing a criticism — but do not soften a real BLOCKER to be agreeable.
