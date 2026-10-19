import { describe, expect, it } from 'vitest';
import { sampleLoss, sampleStats } from './metrics';

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

describe('sampleLoss', () => {
  it('sampleLoss_sumsTheCountersOfEveryInboundVideoStream', async () => {
    // R50: packetsLost, nackCount, pliCount and framesDropped over all of the run's streams.
    const pc = fakePc({
      in1: { type: 'inbound-rtp', kind: 'video', trackIdentifier: 'a', packetsLost: 4, nackCount: 9, pliCount: 1, framesDropped: 2 },
      in2: { type: 'inbound-rtp', kind: 'video', mid: '1', packetsLost: 1, nackCount: 3, pliCount: 0, framesDropped: 0 },
      // A stream that is not tied to a track still counts: the loss happened on the run's connection.
      in3: { type: 'inbound-rtp', mediaType: 'video', packetsLost: 2, nackCount: 0, pliCount: 2, framesDropped: 1 },
      audio: { type: 'inbound-rtp', kind: 'audio', packetsLost: 100 },
      remote: { type: 'remote-inbound-rtp', kind: 'video', packetsLost: 50 },
    });

    expect(await sampleLoss(pc)).toStrictEqual({ rtpLost: 7, nack: 12, pli: 3, framesDropped: 3 });
  });

  it('sampleLoss_counterNoStreamReports_isLeftOut', async () => {
    // Firefox, for example: what the browser does not report is n/a, not 0.
    const pc = fakePc({
      in1: { type: 'inbound-rtp', kind: 'video', packetsLost: 4, nackCount: 2 },
      in2: { type: 'inbound-rtp', kind: 'video', packetsLost: 1 },
    });

    expect(await sampleLoss(pc)).toStrictEqual({ rtpLost: 5, nack: 2 });
  });

  it('sampleLoss_noVideoStream_isEmpty', async () => {
    expect(await sampleLoss(fakePc({}))).toStrictEqual({});
  });
});
