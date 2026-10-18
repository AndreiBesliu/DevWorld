import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, setup, type BindAnswer } from './fakes';
import { runScenario } from './index';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('S9', () => {
  it('S9_h265NotOffered_fallsBackToSub', async () => {
    // A browser without H.265 in its offer: the hub refuses the main stream of
    // mx-h265 and the app falls back to its substream (APP-05). The High 5.1
    // main stream of mx-high51 is offered and plays.
    const t = setup({
      hub: {
        answer: (_slot, cam, profile): BindAnswer =>
          cam === 'mx-h265' && profile === 'main' ? { error: 'CODEC_UNSUPPORTED' } : { codec: 'H264' },
        firstFrameMs: () => 350,
      },
    });

    const result = await finish(runScenario('S9', t.env));

    const binds = t.session().binds.map(({ cam, profile }) => `${cam}/${profile}`);
    expect(binds).toEqual(['mx-h265/main', 'mx-h265/sub', 'mx-high51/main']);
    expect(result.notes).toEqual(
      expect.arrayContaining([
        'mx-h265/main: error CODEC_UNSUPPORTED',
        'mx-h265/sub: bound H264',
        'mx-h265: fallback',
        'mx-high51/main: bound H264',
        'mx-high51: ok',
      ]),
    );
    expect(result.samples['mx-h265.subFirstFrameMs']).toEqual([350]);
    expect(result.samples['mx-h265.firstFrameMs']).toBeUndefined();
    expect(result.samples['mx-high51.firstFrameMs']).toEqual([350]);
    expect(t.stage.said).toContain(t.env.t('h265Unsupported'));
  });

  it('S9_highProfileNotOffered_isUnsupportedProfileWithTheSubFallbackRecorded', async () => {
    // Firefox offers Constrained Baseline only: the hub refuses High as main, and
    // sends the substream in the closest offered profile, which is a fallback, not a failure (R23).
    const t = setup({
      hub: {
        answer: (_slot, cam, profile): BindAnswer => {
          if (profile === 'main') return cam === 'mx-h265' ? { codec: 'H265' } : { error: 'CODEC_UNSUPPORTED' };
          return { codec: 'H264', fallback: '42e01f' };
        },
        // The H.265 stream is bound but never decodes: no first frame within 4 s.
        firstFrameMs: (_i, _slot, cam, profile) => (cam === 'mx-h265' && profile === 'main' ? undefined : 300),
      },
    });

    const result = await finish(runScenario('S9', t.env));

    expect(result.notes).toEqual(
      expect.arrayContaining([
        'mx-h265/main: bound H265',
        'mx-h265/main: no first frame within 4 s',
        'mx-h265/sub: bound H264, fallback profile 42e01f',
        'mx-h265: fallback',
        'mx-high51/main: error CODEC_UNSUPPORTED',
        'mx-high51/sub: bound H264, fallback profile 42e01f',
        'mx-high51: unsupported-profile',
      ]),
    );
    expect(result.samples['mx-high51.subFirstFrameMs']).toEqual([300]);
    expect(t.stage.said).toContain(t.env.t('unsupportedProfile'));
  });
});
