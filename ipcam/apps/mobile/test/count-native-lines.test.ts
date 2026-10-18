// tools/scripts/count-native-lines.sh guards the native code budget of the
// plugins (APP-01, MV14: at most 400 lines per plugin and platform).
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { afterEach, describe, expect, it } from 'vitest';

const script = join(import.meta.dirname, '../../../tools/scripts/count-native-lines.sh');

const dirs: string[] = [];

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

/** A plugin source folder holding `files` (path → content). */
function fixture(files: Record<string, string>): string {
  const dir = mkdtempSync(join(tmpdir(), 'native-lines-'));
  dirs.push(dir);
  for (const [path, content] of Object.entries(files)) {
    mkdirSync(dirname(join(dir, path)), { recursive: true });
    writeFileSync(join(dir, path), content);
  }
  return dir;
}

/** `n` lines of Kotlin code. */
const kotlin = (n: number, from = 0): string =>
  Array.from({ length: n }, (_, i) => `val v${from + i} = ${from + i}\n`).join('');

const count = (...args: string[]) => spawnSync('bash', [script, ...args], { encoding: 'utf8' });

describe('count-native-lines.sh', () => {
  it('countNativeLines_pluginOver400_exits1', () => {
    const plugin = fixture({ 'ro/ipcam/big/A.kt': kotlin(300), 'ro/ipcam/big/B.kt': kotlin(101, 300) });

    const run = count('400', plugin);

    expect(run.status).toBe(1);
    expect(run.stderr).toContain(`${plugin}: 401 lines, over the budget of 400`);
  });

  it('countNativeLines_pluginAt400_exits0AndPrintsTheCount', () => {
    const plugin = fixture({ 'A.kt': kotlin(250), 'B.java': kotlin(150) });

    const run = count('400', plugin);

    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 400 lines (budget 400)`);
  });

  it('countNativeLines_blankAndCommentLinesAndOtherFiles_notCounted', () => {
    const source = [
      '// A line comment.',
      '/**',
      ' * A KDoc block.',
      ' */',
      '',
      '/* One line. */',
      kotlin(3),
      '    ',
      '/*',
      'val hidden = 1',
      '*/',
      'val x = 1 // trailing comment',
    ].join('\n');
    const plugin = fixture({ 'A.kt': source, 'build.gradle': kotlin(500), 'res/layout.xml': kotlin(500) });

    const run = count('400', plugin);

    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 4 lines (budget 400)`);
  });

  it('countNativeLines_eachPluginHasItsOwnBudget', () => {
    const small = fixture({ 'A.kt': kotlin(300) });
    const big = fixture({ 'A.rs': kotlin(301) });

    const run = count('300', small, big);

    expect(run.status).toBe(1);
    expect(run.stdout).toContain(`${small}: 300 lines (budget 300)`);
    expect(run.stderr).toContain(`${big}: 301 lines, over the budget of 300`);
  });

  it('countNativeLines_missingFolderOrBadBudget_exits2', () => {
    expect(count('400', join(tmpdir(), 'no-such-plugin-folder')).status).toBe(2);
    expect(count('many', fixture({ 'A.kt': kotlin(1) })).status).toBe(2);
    expect(count('400').status).toBe(2);
  });
});
