import { resolve } from 'node:path';
import { ESLint } from 'eslint';
import { describe, expect, it } from 'vitest';

// APP-22: @ipcam/player stays free of React and of the Capacitor and Tauri
// shells. These tests run the repo's real ESLint config over fixture sources
// that are placed in the player's src folder, so the rule is proven to fire and
// to stay scoped to the player.

const ipcamRoot = resolve(import.meta.dirname, '../../../..');
const eslint = new ESLint({ cwd: ipcamRoot });

const PLAYER_FILE = 'apps/packages/player/src/fixture.ts';
const OTHER_PACKAGE_FILE = 'apps/packages/core/src/fixture.ts';

async function lint(code: string, file: string): Promise<{ ruleId: string | null; message: string }[]> {
  const [result] = await eslint.lintText(code, { filePath: resolve(ipcamRoot, file) });
  return (result?.messages ?? []).map(({ ruleId, message }) => ({ ruleId, message }));
}

const FORBIDDEN: [string, string][] = [
  ['default import of react', `import React from 'react';\nexport const x = React;\n`],
  ['named import of react', `import { useState } from 'react';\nexport const x = useState;\n`],
  ['type-only import of react', `import type { FC } from 'react';\nexport type X = FC;\n`],
  ['side-effect import of react', `import 'react';\n`],
  ['re-export from react', `export * from 'react';\n`],
  ['type re-export from react', `export type { FC } from 'react';\n`],
  ['import-equals of react', `import React = require('react');\nexport const x = React;\n`],
  ['react subpath', `import { jsx } from 'react/jsx-runtime';\nexport const x = jsx;\n`],
  ['react-dom subpath', `import { createRoot } from 'react-dom/client';\nexport const x = createRoot;\n`],
  ['dynamic import of react', `export const load = () => import('react');\n`],
  ['type-position import of react', `export type Component = import('react').FC;\n`],
  ['typeof import of react', `export type React = typeof import('react');\n`],
  ['type-position import of a react subpath', `export type X = import('react/jsx-runtime').JSX.Element;\n`],
  ['import of Capacitor', `import { Capacitor } from '@capacitor/core';\nexport const x = Capacitor;\n`],
  ['type-position import of Capacitor', `export type P = import('@capacitor/core').Plugin;\n`],
  ['dynamic import of Capacitor', `export const load = () => import('@capacitor/core');\n`],
  ['import of Tauri', `import { invoke } from '@tauri-apps/api/core';\nexport const x = invoke;\n`],
  ['type-position import of Tauri', `export type I = typeof import('@tauri-apps/api/core').invoke;\n`],
  ['dynamic import of Tauri', `export const load = () => import('@tauri-apps/api/core');\n`],
];

const ALLOWED: [string, string][] = [
  ['hls.js', `import Hls from 'hls.js';\nexport const x = Hls;\n`],
  ['a sibling module', `import { createPlayer } from './player';\nexport const x = createPlayer;\n`],
  ['a package whose name starts with react', `import type { X } from 'reactive-streams';\nexport type Y = X;\n`],
  ['a type-position import of such a package', `export type Z = import('reactive-streams').X;\n`],
  ['a dynamic import of hls.js', `export const load = () => import('hls.js');\n`],
  ['a type-position import of hls.js', `export type H = import('hls.js').default;\n`],
];

describe('APP-22 lint rule for @ipcam/player', { timeout: 60_000 }, () => {
  it.each(FORBIDDEN)('lint_playerSource_%s_fails', async (_name, code) => {
    const messages = await lint(code, PLAYER_FILE);

    // Other rules may add messages (import-equals also trips no-require-imports);
    // what matters is that the APP-22 rules report it.
    const app22 = messages.filter(
      ({ ruleId }) => ruleId === 'no-restricted-imports' || ruleId === 'no-restricted-syntax',
    );
    expect(app22.length).toBeGreaterThan(0);
    for (const message of app22) expect(message.message).toContain('APP-22');
  });

  it.each(ALLOWED)('lint_playerSource_%s_passes', async (_name, code) => {
    expect(await lint(code, PLAYER_FILE)).toEqual([]);
  });

  it('lint_otherPackage_mayImportReact', async () => {
    const code = `import type { FC } from 'react';\nexport type X = FC | import('react').ReactNode;\n`;
    expect(await lint(code, OTHER_PACKAGE_FILE)).toEqual([]);
  });
});
