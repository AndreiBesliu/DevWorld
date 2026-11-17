# atelier-ai: project rules for AI sessions

Web app for custom furniture workshops: reads a hand sketch or an architect's PDF with an AI vision model, generates the parts list (BOM), optimizes panel cutting and shows a simple 3D preview. Stay inside this folder (see the root `CLAUDE.md`).

## Current phase: design

- The design spec is in `docs/superpowers/specs/`. Treat it as the source of truth and update it when the owner changes something.
- Do not write product code, scaffold projects or add dependencies until the owner has approved the design spec and an implementation plan for that stage exists in `docs/superpowers/plans/`.

## Architecture rules

- The AI never computes parts. It only extracts parametric cabinets; a deterministic, tested BOM engine computes the parts.
- `backend/app/domain/` and `backend/app/engines/` are pure Python: no FastAPI, no database, no network.
- Dimensions are millimetres as `Decimal` rounded to 0.1 mm. Never use `float` for dimensions.
- Every table carries `workshop_id`, and data access always goes through the current workshop, even while the MVP has no authentication.

## Engineering priorities

- Automated tests for every engine. The reference examples B1 and B2 in the spec are mandatory tests.
- Small, focused units with clear interfaces.
- Prefer mature, permissively licensed (MIT/Apache/BSD) dependencies.

## Language

User-facing docs (README, specs) are in Romanian. Code, identifiers, comments and commit messages are in English. The app's UI text is in Romanian.
