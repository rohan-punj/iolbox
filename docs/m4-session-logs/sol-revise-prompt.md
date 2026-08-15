You are running the PLAN REVISION pass (sol, medium reasoning) for M4 of the
Apple Silicon macOS track for iolbox, in J:\Claude code\iolab-m4-wt.

Read:
1. docs/macos-m4-prompt.md
2. docs/macos-m4-plan.md — the current plan (v1).
3. docs/macos-m4-plan-review.md — an adversarial review of v1 with 13 required
   fixes and a short "what's already good" section.

Rewrite docs/macos-m4-plan.md in place, incorporating every one of the 13
required fixes from the review as concrete, executable plan content (not just
acknowledgement). In particular:
- Change the hardware execution order so the two-hour soak (item 6) is an
  isolated measurement window: VPCS -> multi-link short run -> NAT ->
  extnet disposition -> fresh clean multi-link soak -> four-node -> forced
  termination -> final record. No node/link starts/stops, other phases,
  supervisor/launcher/VM lifecycle commands, fixture reloads, or competing
  harnesses during the 7,200-second window.
- Add the soak-seal manifest mechanism (fix 2), crash/hang/sleep/reboot
  handling with caffeinate/pmset/kern.boottime checks and a watchdog, and the
  "any interruption = restart from zero, no resume/concatenation" rule
  (fix 3).
- Turn the RAM-wall procedure into the deterministic state machine in fix 4
  (exact VM list and order, hard-wall definition, one initial attempt + one
  cold retry, then BLOCKED/UNVERIFIED pending owner).
- Add the per-VM evidence-preservation-before-reuse/deletion procedure
  (fix 5).
- Replace the sampler section with the per-item hardware-evidence matrix from
  fix 6.
- Make completion machine-verifiable per fix 7 (summary.json schema + a
  record-verifier command/description), and fix the "four nodes passed OR
  wall escalated" contradiction — a preserved/escalated wall is an honest
  INCOMPLETE M4, not a completion bar.
- Narrow/mechanize the extnet exception per fix 8 (decision table; a suitable
  Lima interface with an absent product/permission/peer/harness path is
  FAIL/BLOCKED, not NOT EXERCISABLE; remove the invented default-route
  exclusion rule unless the actual extnet contract requires it).
- Add the named requirements matrix with executable gates for every inherited
  hard requirement, per fix 9 (a-h).
- Strengthen "stop never deletes data" into the lifecycle-invariant sentinel
  procedure in fix 10.
- Remove macos-m4-plan.md from the expected-change set, mark it read-only
  during implementation, and add the git diff scope gate from fix 11.
- Add the NAT reachability control-check procedure from fix 12.
- Add the run-identifier/ownership-map cleanup attribution procedure from
  fix 13.

Keep everything the review said the plan already does well (VPCS-first,
9916fb9 not trusted alone, iol22 protection, real HTTP readiness, session
cookie + Origin checks, IOS wake pattern, raw evidence preservation,
conditional-only product changes, four-node-then-forced-termination
ordering).

Do not touch git or implement anything else. When done, overwrite
docs/macos-m4-plan.md with the fully revised plan (this is now v2, final —
say so at the top) and stop.
