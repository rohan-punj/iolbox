# Product

## Register

product

## Users

Primarily **Cisco certification students** — CCNA / CCNP / CCIE candidates who
build topologies to practice on real IOL/IOU images they supply themselves.
Their context is a Windows desktop, often alongside study notes, a config
they're drilling, and limited patience for infrastructure that fights them.
They are learning the domain but are not beginners at computers: they know a
CLI, they know what a switch and a router are, they just want a fast, honest
place to wire nodes, console in, and watch packets.

Secondary: working network engineers and homelabbers validating designs on the
same native-Windows path. When their needs and a student's diverge, keep the
surface legible to the learner without slowing the expert down.

The job to be done: *stand up a working topology, console into every node, and
capture any link — in minutes, on the machine I already have, without a VM
babysitting session or a login.*

## Product Purpose

iolbox is a lightweight, Windows-native lab for Cisco IOL and VPCS. You draw a
topology on a canvas, and it runs the (Linux ELF) IOL binaries inside a small
runtime provider — VMware, WSL2, remote SSH, or software QEMU — talking over
localhost. No database, no web server, no login: a single small app.

It exists because the alternatives (PNetLab / EVE-NG / CML) are heavy VMs that
break nested virtualization and fight the hypervisor you already run. iolbox
keeps the GUI native and pushes only the tiny execution layer into a hypervisor,
so the tool feels like an instrument on your desk, not a data center you have to
administer.

Success looks like: a student opens the app, drags two devices onto the bench,
connects them, hits start, and is consoled in and capturing traffic before they'd
have finished importing an OVA anywhere else — and the whole thing looks and feels
like real gear, not a generic web dashboard.

## Brand Personality

**Instrument. Native. Honest.** (three-word personality)

The tool should feel like professional lab equipment at rest on a bench:
trustworthy, exact, quiet, and calm under load. The design language is grounded
in the subject's own materials — rack equipment, terminal phosphor, LED status
indicators, color-coded patch cabling, the monospace CLI. Voice is direct and
technical without jargon-for-its-own-sake; copy is real and specific ("Drag a
device onto the bench"), never marketing filler or lorem.

The emotional target is **instrument-grade precision**: the user should trust
that what the interface shows is exactly what the runtime is doing. Data the CLI
would print (node names, interfaces, telnet ports, RAM) is set in mono — that is
the one signature move that makes the tool feel native to its audience instead of
like a dashboard. One bold move per surface (the accent, or the glass), and
everything else stays quiet.

## Anti-references

- **"AI-coded default" web templates.** Near-black `#0d1117`, a safe blue accent,
  rounded generic cards, one neutral sans for everything. The scaffold this
  project deliberately replaced. If it could be any SaaS starter, it's wrong.
- **Generic admin dashboards.** iolbox is an instrument, not a metrics panel.
  No hero-metric tiles, no identical card grids, no decorative chrome.
- **The heavy incumbents' UX** — PNetLab / EVE-NG / CML web consoles. We borrow
  their *good* interaction ideas (hover-pop interface labels, connector-on-hover
  link-add, dotted infinite canvas) but not their weight, their VM-admin feel, or
  their dated visual language.
- Status shown as a bare colored word with no shape; gratuitous motion; ad-hoc
  spacing not from the scale; pure-grey/black neutrals with no chosen hue.

## Design Principles

1. **Look like an instrument, not a template.** Ground every visual decision in
   the lab-bench world — phosphor, LEDs, patch cable, rack metal, CLI mono. If a
   choice would fit an unrelated SaaS app equally well, it isn't specific enough.
2. **Mono carries the data; the UI face carries the chrome.** Anything the CLI
   would print is monospace. This is the signature; don't collapse to one neutral
   face for everything.
3. **Status is shape + color, not just a word.** State reads as an LED with a
   soft glow (running / starting / crashed / stopped), legible before you read the
   label — and never by color alone.
4. **One bold move, everything else quiet.** A single accent (cable-cyan on Bench,
   apple-blue on Glass) or the glass material itself is the loud element per
   surface; the rest recedes so the topology stays the subject.
5. **Every animation is functional.** Hover-pop labels, LED pulse, cable
   re-anchor on drag, theme cross-fade — motion earns its place or it's cut, and
   every one has a reduced-motion alternative.
6. **Honest and native over impressive.** Real empty states, real copy, real port
   numbers. The interface should never show something the runtime isn't actually
   doing.

## Accessibility & Inclusion

Target **WCAG 2.1 AA**. Body text ≥ 4.5:1 against its surface; large/bold text
≥ 3:1 — verified on both the Bench (dark slate) and Glass (cool-white) themes,
where the secondary/tertiary ink ramps are deliberately raised/darkened to clear
AA rather than defaulting to elegant-but-washed light gray.

Honor `prefers-reduced-motion` on every animation (hover-pop, LED pulse, cable
re-anchor, theme fade) with a cross-fade or instant-transition alternative — this
is a hard requirement, not a nicety. Interactive controls are keyboard-operable
with a visible `:focus-visible` ring. The connector-on-hover link affordance also
appears on keyboard focus, not hover alone. Status color is backed by LED
shape/glow and text so it survives color-blindness and grayscale.
