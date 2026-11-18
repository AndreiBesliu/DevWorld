import { describe, expect, it, vi } from 'vitest';
import { createAttention, type AudioOut } from './attention';

/** An AudioContext as far as the alert uses it. */
function fakeAudio() {
  const started: number[] = [];
  const context = {
    state: 'suspended' as AudioContextState,
    currentTime: 10,
    destination: {},
    resume: vi.fn(function (this: { state: AudioContextState }) {
      this.state = 'running';
      return Promise.resolve();
    }),
    createOscillator: () => ({
      frequency: { value: 0 },
      connect: (node: unknown) => node,
      start: (at: number) => started.push(at),
      stop: vi.fn(),
    }),
    createGain: () => ({ gain: { value: 0 }, connect: (node: unknown) => node }),
  };
  return { context: context as unknown as AudioOut, started, raw: context };
}

describe('calling the person (S5, S6)', () => {
  it('attention_playsASoundAndVibratesThePhone', async () => {
    const audio = fakeAudio();
    const vibrateNative = vi.fn(() => Promise.resolve());
    const vibrate = vi.fn(() => true);
    const attention = createAttention({ audio: () => audio.context, vibrate, native: { metrics: () => Promise.resolve({}), vibrate: vibrateNative } });

    attention.prime(); // on «Rulează tot…»: browsers start sound only after a tap
    attention.alert();
    await Promise.resolve();

    expect(audio.raw.resume).toHaveBeenCalled();
    expect(audio.started).toEqual([10, 10.3, 10.6]); // three beeps
    expect(vibrateNative).toHaveBeenCalledTimes(1);
    expect(vibrate).not.toHaveBeenCalled(); // the shell's vibration is the one that works in a WebView
  });

  it('attention_inABrowser_vibratesThroughTheVibrationApi', () => {
    const vibrate = vi.fn(() => true);
    const attention = createAttention({ audio: () => undefined, vibrate });

    attention.prime();
    attention.alert();

    expect(vibrate).toHaveBeenCalledWith([400, 200, 400]);
  });

  it('attention_withoutSoundOrVibration_doesNotThrow', () => {
    const attention = createAttention({
      audio: () => {
        throw new Error('no AudioContext');
      },
    });

    attention.prime();
    expect(() => {
      attention.alert();
    }).not.toThrow();
  });
});
