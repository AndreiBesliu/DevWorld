import { afterEach, describe, expect, it, vi } from 'vitest';
import { captureForDecoderInfo, grantCamera } from './decoderinfo';

afterEach(() => {
  vi.useRealTimers();
});

function camera() {
  const track = { stop: vi.fn() };
  const getUserMedia = vi.fn(() => Promise.resolve({ getTracks: () => [track] } as unknown as MediaStream));
  return { track, getUserMedia };
}

describe('captureForDecoderInfo', () => {
  it('decoderInfo_holdsATinyVideoCaptureUntilReleased', async () => {
    const { track, getUserMedia } = camera();

    const release = await captureForDecoderInfo({ getUserMedia });

    expect(getUserMedia).toHaveBeenCalledWith({
      audio: false,
      video: { width: { ideal: 1 }, height: { ideal: 1 }, frameRate: { ideal: 1 } },
    });
    expect(typeof release).toBe('function');
    expect(track.stop).not.toHaveBeenCalled();
    if (typeof release === 'function') release();
    expect(track.stop).toHaveBeenCalledTimes(1);
  });

  it('decoderInfo_noCameraOrNoPermission_isNa', async () => {
    const refused = vi.fn(() => Promise.reject(new DOMException('denied', 'NotAllowedError')));

    expect(await captureForDecoderInfo({ getUserMedia: refused })).toBe('n/a');
    expect(await captureForDecoderInfo(undefined)).toBe('n/a');
  });

  it('decoderInfo_unansweredPrompt_isNaAfter20sAndALateGrantIsStopped', async () => {
    vi.useFakeTimers();
    const { track } = camera();
    let grant: ((stream: MediaStream) => void) | undefined;
    const getUserMedia = vi.fn(
      () =>
        new Promise<MediaStream>((resolve) => {
          grant = resolve;
        }),
    );

    const outcome = captureForDecoderInfo({ getUserMedia });
    await vi.advanceTimersByTimeAsync(20_000);
    expect(await outcome).toBe('n/a');

    grant?.({ getTracks: () => [track] } as unknown as MediaStream);
    await Promise.resolve();
    expect(track.stop).toHaveBeenCalledTimes(1); // nobody holds it: the camera light goes off
  });
});

describe('grantCamera', () => {
  it('camera_oneGetUserMediaPromptAtTheStart_releasedAtOnce', async () => {
    // Post-release task A: the automatic run asks for the camera once, at its start, so that S3's
    // capture windows later hold it without a prompt and the run can be left alone.
    vi.useFakeTimers();
    const { track } = camera();
    let grant: ((stream: MediaStream) => void) | undefined;
    const getUserMedia = vi.fn(
      () =>
        new Promise<MediaStream>((resolve) => {
          grant = resolve;
        }),
    );

    const granted = grantCamera({ getUserMedia });
    await vi.advanceTimersByTimeAsync(90_000); // the person reads the explanation first
    grant?.({ getTracks: () => [track] } as unknown as MediaStream);

    expect(await granted).toBe(true);
    expect(getUserMedia).toHaveBeenCalledTimes(1);
    expect(track.stop).toHaveBeenCalledTimes(1); // the light goes off at once
  });

  it('camera_refusedOrNone_runGoesOn', async () => {
    expect(await grantCamera({ getUserMedia: () => Promise.reject(new DOMException('denied', 'NotAllowedError')) })).toBe(false);
    expect(await grantCamera(undefined)).toBe(false);
  });
});
