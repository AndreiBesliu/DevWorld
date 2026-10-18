import { defineConfig } from 'vitest/config';

// Root entry for running every package's tests in one Vitest process
// (`pnpm exec vitest`). Vitest 4+ ignores vitest.workspace.ts, so the
// workspace is declared through `test.projects`. Keep this list in sync with
// pnpm-workspace.yaml. CI runs each package's own `test` script instead.
export default defineConfig({
  test: {
    projects: ['apps/packages/*', 'apps/mobile', 'apps/desktop', 'tools/spikes/video/app'],
  },
});
