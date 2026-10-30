import { readFileSync, readdirSync } from 'node:fs';
import { join, relative } from 'node:path';
import { expect, it } from 'vitest';

// @ipcam/core is production code: no file in it may carry the marker of spike
// code. The needle is built from two parts so that this file does not contain it.
const SPIKE_ONLY = 'SPIKE' + '-ONLY';

const root = import.meta.dirname;

// Every file of the package, without the installed dependencies.
function packageFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (entry.name === 'node_modules') return [];
    const path = join(dir, entry.name);
    return entry.isDirectory() ? packageFiles(path) : [path];
  });
}

it('core_noSpikeOnlyMarker', () => {
  const files = packageFiles(root).map((path) => relative(root, path));
  expect(files).toContain('package.json');
  expect(files).toContain(join('agerange', 'agerange.ts'));

  const marked = files.filter((file) => readFileSync(join(root, file), 'utf8').includes(SPIKE_ONLY));
  expect(marked).toEqual([]);
});
