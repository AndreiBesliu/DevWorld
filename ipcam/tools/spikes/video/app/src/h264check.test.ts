import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { decodesH264 } from './h264check';
import { ConnectError } from './hubhttp';
import { finish, setup } from './scenarios/fakes';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe('the Firefox preflight', () => {
  it('preflight_aFrameOfTheBandStream_isOk', async () => {
    const t = setup({ fakePlayer: true });

    expect(await finish(decodesH264(t.env))).toBe(true);
    expect(t.session().binds.map(({ cam, profile }) => `${cam}/${profile}`)).toEqual(['sim-01/sub']);
    expect(t.session().closed).toBe(true);
    expect(t.stage.active).toBe(false);
  });

  it('preflight_noFrameWithin10s_isNotOk', async () => {
    // OpenH264 present but not decoding.
    const t = setup({ fakePlayer: true, hub: { firstFrameMs: () => undefined } });

    const started = performance.now();
    expect(await finish(decodesH264(t.env))).toBe(false);
    expect(performance.now() - started).toBeLessThanOrEqual(11_000);
    expect(t.session().closed).toBe(true);
  });

  it('preflight_hubRefusesTheOffer_isNotOk', async () => {
    // Firefox without OpenH264 offers no H.264, and the hub refuses the offer (guide: «rejected»).
    const t = setup({
      fakePlayer: true,
      overrides: {
        openSession: () => Promise.reject(new ConnectError('rejected', 'http://192.168.1.20:7080', 'the hub answered 400: no H.264')),
      },
    });

    expect(await finish(decodesH264(t.env))).toBe(false);
  });

  it('preflight_hubUnreachable_throws', async () => {
    const t = setup({
      fakePlayer: true,
      overrides: { openSession: () => Promise.reject(new ConnectError('refused', 'http://192.168.1.20:7080', 'Failed to fetch')) },
    });

    const failure = await finish(decodesH264(t.env).catch((error: unknown) => error));

    expect(failure).toBeInstanceOf(ConnectError);
  });

  it('preflight_bindRefused_isNotOk', async () => {
    const t = setup({ fakePlayer: true, hub: { answer: () => ({ error: 'CODEC_UNSUPPORTED' }) } });

    expect(await finish(decodesH264(t.env))).toBe(false);
  });
});
