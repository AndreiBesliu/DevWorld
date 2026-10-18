import { existsSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { defineConfig } from 'vitest/config';

// Root entry for running every package's tests in one Vitest process
// (`pnpm exec vitest`) and for editor integrations. CI and `pnpm test` run each
// package's own `test` script instead; a package run uses its own folder as the
// root and never reads this file. Vitest 4+ ignores vitest.workspace.ts, so the
// projects are declared through `test.projects`.
//
// The list mirrors pnpm-workspace.yaml but is not generated from it: packages
// that are not Vitest suites (the Playwright e2e package, for example) must
// stay out. Vitest 5 fails at startup on a literal project path that does not
// exist and on an empty `projects` list, so only folders that hold a package
// are listed, and `projects` is omitted while there are none.
const root = import.meta.dirname;

const isPackage = (dir: string): boolean => existsSync(join(root, dir, 'package.json'));

const packagesIn = (parent: string): string[] =>
  existsSync(join(root, parent))
    ? readdirSync(join(root, parent), { withFileTypes: true })
        .filter((entry) => entry.isDirectory())
        .map((entry) => `${parent}/${entry.name}`)
        .filter(isPackage)
    : [];

const projects = [
  ...packagesIn('apps/packages'),
  ...['apps/mobile', 'apps/desktop', 'tools/spikes/video/app'].filter(isPackage),
];

export default defineConfig({
  test: {
    ...(projects.length > 0 ? { projects } : {}),
  },
});
