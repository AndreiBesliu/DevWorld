import { readFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { Decrypter, Encrypter } from 'age-encryption';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createI18n } from '../i18n';
import { ResultsQueue, SCENARIO_IDS } from '../results';
import { ageScenario, BENCH_BYTES, benchAgerange, MIN_MBPS, seededBytes } from './age';
import { setup } from './fakes';
import { SCENARIOS } from './index';
import { runDefinition } from './runner';

// Task 2's golden files, which the Go writer wrote (ENC-07): the bench reads them in place.
const goldenDir = resolve(import.meta.dirname, '../../../../../../hub/internal/crypto/testdata/golden');

interface GoldenVector {
  file: string;
  fileKeyHex: string;
  plaintextSize: number;
}

const fromHex = (hex: string): Uint8Array => new Uint8Array(Buffer.from(hex, 'hex'));

afterEach(() => {
  vi.restoreAllMocks();
});

describe('AGE', () => {
  it('ageScenario_benchDecryptsGoldenAndComputesMbps', async () => {
    const vectors = JSON.parse(readFileSync(join(goldenDir, 'vectors.json'), 'utf8')) as GoldenVector[];
    // 17 chunks: 16 of 64 KiB and one of 17 bytes.
    const golden = vectors.find((v) => v.plaintextSize === 16 * 65536 + 17);
    if (!golden) throw new Error('no 17-chunk golden file');
    const cipher = new Uint8Array(readFileSync(join(goldenDir, golden.file)));
    const fileKey = fromHex(golden.fileKeyHex);

    // The clock is read once before agerange opens the file and once after
    // the whole plaintext is read: its bits over that time are the rate.
    for (const [mbps, pass] of [
      [11, false],
      [13, true],
    ] as const) {
      const ms = (golden.plaintextSize * 8) / 1000 / mbps;
      const reads = [5000, 5000 + ms];
      const result = await benchAgerange(cipher, fileKey, () => {
        const t = reads.shift();
        if (t === undefined) throw new Error('the clock was read more than twice');
        return t;
      });
      expect(result.mbps).toBeCloseTo(mbps, 9);
      expect(result.pass).toBe(pass);
    }
    expect(MIN_MBPS).toBe(12);

    // With another key nothing decrypts: no rate.
    const wrong = fileKey.slice();
    wrong[0]! ^= 1;
    await expect(benchAgerange(cipher, wrong, () => 0)).rejects.toThrow(/authenticate/);
  });

  it('ageScenario_encryptsWithTypageThenTimesAgerange', async () => {
    const encrypt = vi.spyOn(Encrypter.prototype, 'encrypt');
    const decryptHeader = vi.spyOn(Decrypter.prototype, 'decryptHeader');
    let now = 0;
    const t = setup({
      overrides: {
        // Each read of the clock is 100 ms after the one before.
        clock: {
          now: () => (now += 100),
          setTimer: (fn, ms) => {
            const id = setTimeout(fn, ms);
            return () => {
              clearTimeout(id);
            };
          },
        },
      },
    });

    const result = await runDefinition('AGE', ageScenario(2 ** 20), t.env);

    expect(result.notes.filter((note) => note.startsWith('failed'))).toEqual([]);
    // typage encrypts the fixed-seed buffer in the page and unwraps the file key from the header alone.
    expect(encrypt).toHaveBeenCalledTimes(1);
    // Compared as a Buffer: toEqual walks a 1 MiB array element by element, for seconds.
    const encrypted = encrypt.mock.calls[0]![0];
    expect(encrypted instanceof Uint8Array && Buffer.from(encrypted).equals(seededBytes(2 ** 20))).toBe(true);
    expect(decryptHeader).toHaveBeenCalledTimes(1);
    const header = decryptHeader.mock.calls[0]![0];
    expect(new TextDecoder().decode(header).startsWith('age-encryption.org/v1\n')).toBe(true);
    expect(new TextDecoder().decode(header).endsWith('\n')).toBe(true);
    // agerange is timed alone: one 100 ms step of the clock for 1 MiB. The
    // figures are at the top of the result, {scenario: "AGE", mbps, bytes, ms},
    // beside the device facts that every result carries.
    expect(result).toMatchObject({ scenario: 'AGE', mbps: (2 ** 20 * 8) / 1000 / 100, bytes: 2 ** 20, ms: 100 });
    expect(result.samples).toEqual({});
    expect(t.sessions).toHaveLength(0);
  });

  it('ageScenario_postsScenarioMbpsBytesMsToTheHub', async () => {
    // The phase0c1 plan's contract: the app posts {scenario: "AGE", mbps, bytes, ms}
    // to <hub>/spike/v1/results, where reccontroller keeps it under results/app/.
    let now = 0;
    const t = setup({
      overrides: {
        clock: {
          now: () => (now += 50),
          setTimer: (fn, ms) => {
            const id = setTimeout(fn, ms);
            return () => {
              clearTimeout(id);
            };
          },
        },
      },
    });
    const result = await runDefinition('AGE', ageScenario(2 ** 16), t.env);
    const posted: { url: string; body: string }[] = [];
    const queue = new ResultsQueue({
      fetch: (input, init) => {
        posted.push({ url: String(input), body: String(init?.body) });
        return Promise.resolve(new Response(null, { status: 201 }));
      },
      hubUrl: () => 'http://pc.test:7080/',
    });

    queue.add(result);

    expect(await queue.flush()).toBe(0);
    expect(posted.map((p) => p.url)).toEqual(['http://pc.test:7080/spike/v1/results']);
    expect(JSON.parse(posted[0]!.body)).toMatchObject({
      scenario: 'AGE',
      mbps: (2 ** 16 * 8) / 1000 / 50,
      bytes: 2 ** 16,
      ms: 50,
      device: { platform: 'chrome' },
    });
  });

  it('ageScenario_isListedWithItsRomanianLabel', () => {
    expect(SCENARIO_IDS).toContain('AGE');
    expect(SCENARIOS.AGE).toBeDefined();
    expect(BENCH_BYTES).toBe(32 * 2 ** 20);
    expect(createI18n({ lang: 'ro' }).t('descAGE')).toBe('Viteză agerange (MI4)');
    // The fixed seed gives the same bytes at every run, and not a constant.
    expect(seededBytes(64)).toEqual(seededBytes(64));
    expect(new Set(seededBytes(64)).size).toBeGreaterThan(16);
  });
});
