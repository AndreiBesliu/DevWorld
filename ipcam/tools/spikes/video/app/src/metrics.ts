/** What one received video track reports. */
export interface TileStats {
  framesDecoded: number;
  framesDropped: number;
  /** 0 when the browser reports none; the frame counts of two samples give the rate then. */
  fps: number;
  /** Chrome only, and only while a camera capture is held (MV4): the decoder behind the track. */
  decoder?: string;
  /** Chrome only, as above: whether that decoder is power efficient (hardware). */
  powerEfficient?: boolean;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

const count = (value: unknown): number => (typeof value === 'number' ? value : 0);

function tileStats(stat: Record<string, unknown>): TileStats {
  const tile: TileStats = {
    framesDecoded: count(stat.framesDecoded),
    framesDropped: count(stat.framesDropped),
    fps: count(stat.framesPerSecond),
  };
  if (typeof stat.decoderImplementation === 'string' && stat.decoderImplementation !== '') {
    tile.decoder = stat.decoderImplementation;
  }
  if (typeof stat.powerEfficientDecoder === 'boolean') tile.powerEfficient = stat.powerEfficientDecoder;
  return tile;
}

/**
 * Samples the `inbound-rtp` video streams of a PeerConnection, keyed by the id
 * of the MediaStreamTrack that carries them (`SpikeSession.track(slot).id`). A
 * browser that does not name the track in the stream is read through the mid of
 * its transceiver. A stream that cannot be tied to a track is left out.
 */
export async function sampleStats(pc: RTCPeerConnection): Promise<Map<string, TileStats>> {
  const report = await pc.getStats();
  const trackByMid = new Map<string, string>();
  for (const transceiver of pc.getTransceivers()) {
    if (transceiver.mid !== null) trackByMid.set(transceiver.mid, transceiver.receiver.track.id);
  }

  const tiles = new Map<string, TileStats>();
  report.forEach((stat: unknown) => {
    if (!isRecord(stat) || stat.type !== 'inbound-rtp') return;
    if ((stat.kind ?? stat.mediaType) !== 'video') return;
    const trackId =
      typeof stat.trackIdentifier === 'string'
        ? stat.trackIdentifier
        : typeof stat.mid === 'string'
          ? trackByMid.get(stat.mid)
          : undefined;
    if (trackId !== undefined) tiles.set(trackId, tileStats(stat));
  });
  return tiles;
}
