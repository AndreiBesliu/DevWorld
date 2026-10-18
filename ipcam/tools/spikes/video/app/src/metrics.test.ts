import { describe, expect, it } from 'vitest';
import { sampleStats } from './metrics';

type Stat = Record<string, unknown>;

/** A PeerConnection with the two calls sampleStats makes. */
function fakePc(stats: Record<string, Stat>, transceivers: { mid: string | null; trackId: string }[] = []): RTCPeerConnection {
  const report = new Map<string, Stat>(Object.entries(stats));
  return {
    getStats: () => Promise.resolve(report),
    getTransceivers: () => transceivers.map((t) => ({ mid: t.mid, receiver: { track: { id: t.trackId } } })),
  } as unknown as RTCPeerConnection;
}

describe('sampleStats', () => {
  it('sampleStats_mapsInboundRtp', async () => {
    const pc = fakePc({
      in1: {
        type: 'inbound-rtp',
        kind: 'video',
        trackIdentifier: 'track-a',
        framesDecoded: 240,
        framesDropped: 3,
        framesPerSecond: 11.8,
        decoderImplementation: 'ExternalDecoder',
        powerEfficientDecoder: true,
      },
      in2: {
        type: 'inbound-rtp',
        kind: 'video',
        trackIdentifier: 'track-b',
        framesDecoded: 90,
        framesDropped: 0,
        framesPerSecond: 20,
      },
      // Not video, not inbound, not a stream at all: all ignored.
      audio: { type: 'inbound-rtp', kind: 'audio', trackIdentifier: 'track-audio', framesDecoded: 1 },
      out: { type: 'outbound-rtp', kind: 'video', trackIdentifier: 'track-out', framesEncoded: 5 },
      codec: { type: 'codec', mimeType: 'video/H264' },
      transport: { type: 'transport', bytesReceived: 100 },
    });

    const stats = await sampleStats(pc);

    expect([...stats.keys()]).toEqual(['track-a', 'track-b']);
    expect(stats.get('track-a')).toStrictEqual({
      framesDecoded: 240,
      framesDropped: 3,
      fps: 11.8,
      decoder: 'ExternalDecoder',
      powerEfficient: true,
    });
    // The decoder fields exist in Chrome only, and only while a capture is held: absent, not undefined.
    expect(stats.get('track-b')).toStrictEqual({ framesDecoded: 90, framesDropped: 0, fps: 20 });
  });

  it('sampleStats_powerEfficientFalse_isKept', async () => {
    const pc = fakePc({
      in1: { type: 'inbound-rtp', kind: 'video', trackIdentifier: 't', framesDecoded: 1, powerEfficientDecoder: false },
    });

    expect((await sampleStats(pc)).get('t')?.powerEfficient).toBe(false);
  });

  it('sampleStats_missingCounters_areZero', async () => {
    const pc = fakePc({ in1: { type: 'inbound-rtp', kind: 'video', trackIdentifier: 't' } });

    expect((await sampleStats(pc)).get('t')).toStrictEqual({ framesDecoded: 0, framesDropped: 0, fps: 0 });
  });

  it('sampleStats_olderMediaTypeField_isVideoToo', async () => {
    const pc = fakePc({
      in1: { type: 'inbound-rtp', mediaType: 'video', trackIdentifier: 'v', framesDecoded: 7 },
      in2: { type: 'inbound-rtp', mediaType: 'audio', trackIdentifier: 'a', framesDecoded: 7 },
    });

    expect([...(await sampleStats(pc)).keys()]).toEqual(['v']);
  });

  it('sampleStats_noTrackIdentifier_usesTheTrackOfTheTransceiverWithThatMid', async () => {
    const pc = fakePc(
      {
        in1: { type: 'inbound-rtp', kind: 'video', mid: '2', framesDecoded: 50 },
        in2: { type: 'inbound-rtp', kind: 'video', mid: '9', framesDecoded: 60 }, // no such transceiver
        in3: { type: 'inbound-rtp', kind: 'video', framesDecoded: 70 }, // no way to tell the tile
      },
      [
        { mid: '1', trackId: 'track-x' },
        { mid: '2', trackId: 'track-y' },
        { mid: null, trackId: 'track-z' },
      ],
    );

    const stats = await sampleStats(pc);

    expect([...stats.keys()]).toEqual(['track-y']);
    expect(stats.get('track-y')?.framesDecoded).toBe(50);
  });

  it('sampleStats_noStreams_isEmpty', async () => {
    expect((await sampleStats(fakePc({}))).size).toBe(0);
  });
});
