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

  it('countNativeLines_codeAfterAClosingComment_countsAsCode', () => {
    const source = [
      '/* c */ val a = 1',
      '/** doc */ fun f() {}',
      '/* only */ /* comments */',
      '/* a */ /* b */ val z = 2',
      '/*',
      ' * A block that ends on a line with code.',
      ' */ val b = 3',
      '/* closes at once */',
    ].join('\n');
    const plugin = fixture({ 'A.kt': source });

    const run = count('400', plugin);

    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 4 lines (budget 400)`);
  });

  it('countNativeLines_blockOpenedAfterCode_theCommentLinesAreNotCounted', () => {
    const source = [
      'val y = 3 /* starts here',
      'a comment line',
      'val hidden = 1',
      'ends here */',
      'val z = 4',
      'val w = 5 /* inline */ + 6',
      'val v = 7 /* opens again',
      '   and closes */ val u = 8',
      'val t = 9',
    ].join('\n');
    const plugin = fixture({ 'A.kt': source });

    const run = count('400', plugin);

    // y, z, w, v, u (the line that closes the block holds code) and t.
    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 6 lines (budget 400)`);
  });

  it('countNativeLines_commentMarkersInsideStrings_areCode', () => {
    const source = [
      'val mime = "video/*"', // a MIME type, not the start of a comment
      'val escaped = "a \\" /* b"',
      'val next = 1',
      'val url = "http://host" // trailing',
    ].join('\n');
    const plugin = fixture({ 'A.kt': source });

    const run = count('400', plugin);

    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 4 lines (budget 400)`);
  });

  it('countNativeLines_multiLineRawString_isCodeAndItsCommentMarkersAreText', () => {
    // A Kotlin, Java or Swift raw string spans lines; a /* inside it opens no comment.
    const source = [
      'val s = """',
      '  some /* text',
      '  // and more',
      '"""',
      'val t = 1',
      'val u = "" // an empty string is no raw string',
      '/* a real comment',
      'val hidden = 1 */',
      'val v = """one line /* """',
      'val w = 2',
    ].join('\n');
    const plugin = fixture({ 'A.kt': source });

    const run = count('400', plugin);

    // The raw string takes 4 lines, then t, u, v and w.
    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 8 lines (budget 400)`);
  });

  it('countNativeLines_rustStringOverSeveralLines_isCodeAndItsCommentMarkersAreText', () => {
    // A Rust "..." string may span lines (Task 10b review, m1); a /* inside it opens no comment.
    const source = ['let s = "line one', '  /* inside a string', '  still";', 'let t = 1;', 'let u = 2;'].join('\n');
    const plugin = fixture({ 'a.rs': source, 'B.kt': 'val k = "kotlin strings end at the line end\n/* c */\nval l = 1' });

    const run = count('400', plugin);

    // The string takes 3 lines, then t and u; k and l in the Kotlin file.
    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 7 lines (budget 400)`);
  });

  it('countNativeLines_rustRawString_isCodeAndItsQuotesAndCommentMarkersAreText', () => {
    const source = [
      'let js = r#"',
      '  const q = "a"; /* not a comment',
      '"#;',
      'let b = r"\\"; /* a comment */', // r"\" holds one backslash, which escapes nothing
      'let c = br"x"; // trailing',
      'let d = 1;',
    ].join('\n');
    const plugin = fixture({ 'a.rs': source });

    const run = count('400', plugin);

    // The raw string takes 3 lines, then b, c and d.
    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 6 lines (budget 400)`);
  });

  it('countNativeLines_quoteCharLiteral_opensNoString', () => {
    // A '"' character literal is no string (Task 10b review, m1); Rust lifetimes are no literals.
    const kt = ["val q = '\"'; val s = \"\"\"", '  text /* inside', '  more', '"""', 'val t = 1'].join('\n');
    const rs = ["let q = '\"';", "let e = '\\\"'; /* c */", "fn f<'a>(s: &'a str) {}", '/* c */', 'let x = 1;'].join('\n');
    const plugin = fixture({ 'A.kt': kt, 'a.rs': rs });

    const run = count('400', plugin);

    // The Kotlin raw string takes 4 lines, then t; q, e, f and x in the Rust file.
    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 9 lines (budget 400)`);
  });

  it('countNativeLines_budgetWithALeadingZero_isDecimal', () => {
    // Bash reads 0400 as octal 256 in arithmetic, and 08 not at all.
    const plugin = fixture({ 'A.kt': kotlin(300) });

    const run = count('0400', plugin);

    expect(run.status).toBe(0);
    expect(run.stdout).toContain(`${plugin}: 300 lines (budget 400)`);
    expect(count('08', fixture({ 'A.kt': kotlin(8) })).status).toBe(0);
    expect(count('08', fixture({ 'A.kt': kotlin(9) })).status).toBe(1);
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
