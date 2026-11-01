# furniture-pro: project rules for AI sessions

Commercial Windows application for furniture workshops and factories. Product name: Furniture Pro. Stay inside this folder (see the root `CLAUDE.md`).

## Current phase: design

- Requirements are in `docs/brief.md`. Treat them as the source of truth and update them when the owner changes something.
- The architecture proposal is `docs/superpowers/specs/2026-11-01-furniture-pro-arhitectura-design.md`. It is NOT approved yet; its status line says so.
- Do not write product code, scaffold projects or add dependencies until the owner has approved the design spec and an implementation plan exists in `docs/superpowers/plans/`.

## Owner's mandatory development rules

1. Never build the whole application in one stage. Work stage by stage.
2. Split the product into independent modules.
3. Settle the architecture and the database structure before implementing.
4. Write technical documentation for every module.
5. Write automated tests for every calculation formula.
6. Never use fictitious values in production calculations. Missing data is a visible error, never a silent zero or an invented default. Catalog data (prices, hardware dimensions, manufacturer formulas) comes from the owner or from official manufacturer sources, never from memory.
7. Never duplicate calculation logic between the UI and the backend. All calculations live in the core engine; the UI only displays results and sends commands.
8. Keep modules compatible: versioned contracts between modules, versioned file formats with migrations.
9. Save restore points: commit after every finished step, push to GitHub, tag every approved stage (`furniture-pro/etapa-N-aprobata`).
10. Explain commands and technical steps to the owner in plain Romanian. Assume no programming knowledge.
11. Never start the next stage before the owner has verified the current one.
12. Never change a working module without first explaining the reason and the effects to the owner.

## Language

User-facing docs (README, brief, specs, plans, module docs) are in Romanian. Code, identifiers, code comments and commit messages are in English.
