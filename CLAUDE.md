# DevWorld: rules for every AI session

This repo hosts several independent projects, one per top-level folder. Several AI sessions work here at the same time, each on its own project.

- Work on exactly one project per session. Only create, edit, move or delete files inside that project's folder.
- Never modify another project's folder. If you need something from it, ask the owner.
- Root files (`README.md`, this file, `.github/`) are shared. Change them only for repo-wide conventions, keep the edit minimal, and call it out in the PR.
- GitHub Actions workflows must live in `.github/workflows/` as `<project>-*.yml`, with `paths:` filters scoped to the project's folder.
- Each project is self-contained: its own dependencies, build, tests and docs. Read the project's own `CLAUDE.md` before working.
- Use one branch per task and open a PR. Never force-push `main`.
- When you add a project, add a row to the table in `README.md`.
- The owner writes in Romanian: reply to them in Romanian. Code, identifiers, code comments and commit messages are in English.
