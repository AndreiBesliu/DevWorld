import type { HlsLike } from '@ipcam/player';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { finish, setup, TEST_HUB, type FakeVideo } from './fakes';
import { runScenario } from './index';

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

/** hls.js as far as the player uses it: the playlist shows its first frame `startMs` after loadSource. */
class FakeHls implements HlsLike {
  media: HTMLMediaElement | null = null;
  readonly levels = [];
  readonly currentLevel = -1;
  static loaded: string[] = [];

  constructor(private readonly startMs: number) {}

  attachMedia(media: HTMLMediaElement): void {
    this.media = media;
  }

  loadSource(url: string): void {
    FakeHls.loaded.push(url);
    const media = this.media as unknown as FakeVideo;
    setTimeout(() => {
      media.present();
    }, this.startMs);
  }

  destroy(): void {
    this.media = null;
  }
}

describe('S7', () => {
  it('S7_timesTwentyStartsAndTwentySeeksFromTheSeekedEvent', async () => {
    FakeHls.loaded = [];
    let start = 0;
    const t = setup({ playerOptions: { hlsFactory: () => new FakeHls(400 + 10 * start++) } });
    const seekTargets: number[] = [];
    const running = runScenario('S7', t.env);
    await vi.advanceTimersByTimeAsync(10);
    const video = t.stage.all[0] as FakeVideo;
    video.seekMs = 250;
    video.addEventListener('seeked', () => seekTargets.push(video.currentTime));
    const result = await finish(running);

    expect(result.samples.startMs).toEqual(Array.from({ length: 20 }, (_, i) => 400 + 10 * i));
    expect(result.samples.seekMs).toEqual(Array<number>(20).fill(250));
    expect(FakeHls.loaded).toEqual(Array<string>(20).fill(`${TEST_HUB}/media/hls/index.m3u8`));
    // random() is 0.5: the middle of the 10 min recording, less the last 10 s.
    expect(seekTargets).toEqual(Array<number>(20).fill(295));
    expect(t.sessions).toHaveLength(0); // HLS needs no WebRTC session
    expect(JSON.stringify(result)).not.toContain(TEST_HUB);
  });
});
