# ipcam: project rules for AI sessions

Commercial cross-platform IP camera app. Working name `ipcam` until the product name is chosen. Stay inside this folder (see the root `CLAUDE.md`).

## Current phase: Phase 0 (risk probes)

- The design spec `docs/superpowers/specs/2026-10-17-ipcam-design.md` was approved by the owner on 2026-10-18. It is the source of truth; `docs/brief.md` summarises the owner's decisions.
- Work only from an implementation plan in `docs/superpowers/plans/`. Code outside an approved plan needs the owner's go-ahead.
- Phase 0 costs nothing (O13): no paid services, accounts or payment details. Anything paid or iOS-related belongs to "Faza 0-bis" (spec §16.9).

## Engineering priorities

The owner relies entirely on AI to build and maintain this product, so optimize for what keeps AI-maintained code reliable:

- Strongly typed languages, and as few languages across the system as possible.
- Automated tests for every component. CI must build and test every platform; nothing ships that CI has not built.
- Small, focused units with clear interfaces. Structured logs and diagnostics that make field problems debuggable without a human developer.
- Prefer mature, permissively licensed (MIT/Apache/BSD) dependencies. GPL/AGPL code must not end up in the shipped product.

## Language

User-facing docs (README, brief, specs) are in Romanian. Code, identifiers, comments and commit messages are in English.
