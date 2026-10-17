# ipcam: project rules for AI sessions

Commercial cross-platform IP camera app. Working name `ipcam` until the product name is chosen. Stay inside this folder (see the root `CLAUDE.md`).

## Current phase: design

- Agreed requirements are in `docs/brief.md`. Treat them as the source of truth and update them when the owner changes something.
- Do not write product code, scaffold projects or add dependencies until the owner has approved the design spec in `docs/superpowers/specs/` and an implementation plan exists in `docs/superpowers/plans/`.

## Engineering priorities

The owner relies entirely on AI to build and maintain this product, so optimize for what keeps AI-maintained code reliable:

- Strongly typed languages, and as few languages across the system as possible.
- Automated tests for every component. CI must build and test every platform; nothing ships that CI has not built.
- Small, focused units with clear interfaces. Structured logs and diagnostics that make field problems debuggable without a human developer.
- Prefer mature, permissively licensed (MIT/Apache/BSD) dependencies. GPL/AGPL code must not end up in the shipped product.

## Language

User-facing docs (README, brief, specs) are in Romanian. Code, identifiers, comments and commit messages are in English.
