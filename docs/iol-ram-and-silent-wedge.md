# IOL RAM, and the "running but wedged" failure mode

## The failure

An IOL node given too little memory does not fail. It *wedges*, and it wedges
in the one way the supervisor cannot see.

Confirmed on real hardware, 2026-08-14, two IOL 17.18.02 nodes at `ram: 256`:

```
%SYS-2-MALLOCFAIL: Memory allocation of 220004 bytes failed
Pool: Processor  Free: 21216  Cause: Not enough free memory
```

IOS died partway through init. The IOL **process** did not. So:

- `lab.start` returned `ok: true`.
- Both nodes reported state `running`, and stayed there.
- Nothing appeared in the supervisor logs.
- Nothing appeared in the control protocol.

The only evidence anywhere was on the node's console. Raising each node to
`ram: 1024` fixed it.

## Why 256 shows up at all

256 MB is IOL's own built-in default — what it uses when no `-m` flag is
passed. It is a fine number for the 15.x images IOL was written against and far
too small for a modern 17.x x86_64 image. It leaks into labs from three
directions:

1. A lab document with no `ram` field at all (`node.ram == 0`), which used to
   mean "omit `-m`" and therefore "let IOL pick 256".
2. EVE-NG / PNetLab `.unl` packs, which routinely carry `ram="256"`.
3. Hand-authored labs copying either of the above.

## What the supervisor does now

`node.IOLRAMFor(ram, class)` (`supervisor/internal/node/argv.go`) resolves every
IOL node's `-m` value against a per-class floor, `node.MinIOLRAMMB` — **1024 MB**
for both `l2` and `l3` today. `buildSpec` calls it, so:

- `ram` unset (0) → the class floor. `-m` is now always passed for IOL; IOL's
  own 256 MB default is never reached.
- `ram` below the floor → raised to the floor. This overrides the lab author on
  purpose: below the floor the node does not run slower, it runs *inert*, and a
  node that reports `running` while IOS is dead is worse than one that ignores
  a number you typed.
- `ram` at or above the floor → passed through untouched.

`lab.load` emits a warning per node it corrected, so the bump is visible in the
GUI's load result rather than being silently applied. Document validation
(`lab.Validate`) deliberately still accepts anything `>= 32`: old lab documents
must keep loading, and the floor is applied at spawn where the image class is
actually known.

The GUI's RAM field and `contracts/lab.schema.json` both advertise 1024 as the
minimum and default. `labs/import-pack.py` raises imported `.unl` values to the
same floor and prints a warning for each one it touches.

## What is still not surfaced

The RAM floor removes the *known* cause of a silent wedge. It does not detect
the general case: **a node whose state is `running` but whose console has never
reached a usable prompt is still indistinguishable from a healthy node.** Any
other init-time IOS failure — a bad startup-config, a licensing stall, an image
that will not run on this arch — presents exactly the same way.

Nothing in the current control protocol carries boot health. `node.state`
tracks the *process*, which is not the same claim.

If this is worth closing, the shape is roughly:

- The supervisor already sees every console byte (`node/console_hub.go`), and
  `internal/consolescript` already knows how to recognise an IOS prompt.
- A per-node watcher could subscribe at spawn, look for a prompt within a
  deadline, and separately match known-fatal console patterns
  (`%SYS-2-MALLOCFAIL`, and friends).
- Result: either a new `node.bootHealth` event, or a third state between
  `starting` and `running`, so `running` can mean "reached a prompt" instead of
  "the process exists".

Until then, if a node is `running` and unresponsive, **open its console** — the
supervisor has no other channel that would tell you.
