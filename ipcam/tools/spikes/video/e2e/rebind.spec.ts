// HUB-06 quick re-binds (Tasks 9b and 9c, adopted from the SDD check script
// rebind-check.cjs): re-binds of one slot must show each bind's own first frame
// at once, and the frames that follow must not be held.
//
// One PeerConnection with 2 recvonly slots and the rpc channel, straight
// against the hub's wire protocol (session package doc), in Chrome: Firefox
// reports no receiveTime in requestVideoFrameCallback, which tells a frame of
// this bind from one of the bind before. The element stays attached; per bind:
// - b2b (like S4 paging): unbind and bind at once;
// - switch (like a camera switch): bind without the unbind;
// alternating sim-01/sub and sim-02/sub, playing 1 s plus a random 0 to 84 ms,
// so that the binds fall at every phase of the cameras' frames (12 fps: one
// frame every 83.3 ms).
//
// A bind passes when its first presented frame arrived after the bind (rVFC
// receiveTime ≥ bind time) and was presented within 100 ms of it, and the
// median time from a frame's arrival (receiveTime) to its display
// (expectedDisplayTime), over the frames of the bind's 1 s, is within 100 ms:
// an RTP clock that runs ahead of the hub's makes Chrome hold frames by the lead.
import { expect, test } from '@playwright/test';

const BINDS = 30;
const LIMIT_MS = 100;
const PLAY_MS = 1000;
const JITTER_MS = 84;

interface Bind {
  epoch: number;
  cam: string;
  /** The bind's first frame: display and arrival, in ms after the bind; null when none came. */
  first: { ms: number; receivedMs: number } | null;
  /** Frames presented after the bind but received before it (the previous bind's). */
  staleBeforeFirst: number;
  frames: number;
  /** Arrival to display, per frame of this bind. */
  holdMs: number[];
  /** The receiver's counters over the bind, to tell a dropped or undecodable frame from a held one. */
  counters: Record<string, number>;
}

const median = (values: number[]): number | undefined => [...values].sort((a, b) => a - b)[Math.floor(values.length / 2)];

for (const mode of ['b2b', 'switch'] as const) {
  test(`HUB-06 re-binds (${mode}): each bind's own first frame and its frames within ${LIMIT_MS} ms`, { tag: '@chrome' }, async ({ page }) => {
    // Same origin as the hub, for its offer endpoint.
    await page.goto('/spike/v1/cameras');
    const { binds, hubErrors } = await page.evaluate(
      async ({ count, mode, playMs, jitterMs }) => {
        const pc = new RTCPeerConnection({ iceServers: [] });
        const slot0 = pc.addTransceiver('video', { direction: 'recvonly' });
        pc.addTransceiver('video', { direction: 'recvonly' });
        const dc = pc.createDataChannel('rpc');
        const hubErrors: string[] = [];
        dc.onmessage = (event: MessageEvent<string>) => {
          if (event.data.includes('"t":"error"')) hubErrors.push(event.data);
        };
        await pc.setLocalDescription(await pc.createOffer());
        await new Promise<void>((resolve) => {
          if (pc.iceGatheringState === 'complete') resolve();
          pc.onicegatheringstatechange = () => {
            if (pc.iceGatheringState === 'complete') resolve();
          };
          setTimeout(resolve, 3000);
        });
        const answer = await fetch('/spike/v1/offer', {
          method: 'POST',
          body: pc.localDescription?.sdp ?? '',
          headers: { 'Content-Type': 'text/plain' },
        });
        if (!answer.ok) throw new Error(`the hub refused the offer: ${answer.status} ${await answer.text()}`);
        await pc.setRemoteDescription({ type: 'answer', sdp: await answer.text() });
        await new Promise<void>((resolve) => {
          if (dc.readyState === 'open') resolve();
          else dc.onopen = () => resolve();
        });

        const video = document.createElement('video');
        video.muted = true;
        video.playsInline = true;
        document.body.append(video);
        const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
        const round = (ms: number) => Math.round(ms * 10) / 10;

        // Every presented frame, on the performance clock.
        const presented: { display: number; received: number | null }[] = [];
        const onFrame: VideoFrameRequestCallback = (now, metadata) => {
          presented.push({ display: metadata.expectedDisplayTime ?? now, received: metadata.receiveTime ?? null });
          video.requestVideoFrameCallback(onFrame);
        };
        video.requestVideoFrameCallback(onFrame);
        video.srcObject = new MediaStream([slot0.receiver.track]);
        video.play().catch(() => undefined);

        const COUNTERS = ['framesReceived', 'framesDecoded', 'framesDropped', 'keyFramesDecoded', 'pliCount', 'nackCount', 'packetsLost', 'freezeCount'];
        const inbound = async (): Promise<Record<string, number>> => {
          let found: Record<string, number> = {};
          (await slot0.receiver.getStats()).forEach((stat: Record<string, unknown>) => {
            if (stat.type !== 'inbound-rtp') return;
            found = Object.fromEntries(COUNTERS.map((key) => [key, typeof stat[key] === 'number' ? stat[key] : 0]));
          });
          return found;
        };

        const binds: Bind[] = [];
        let before = await inbound();
        for (let epoch = 1; epoch <= count; epoch++) {
          const cam = epoch % 2 === 1 ? 'sim-01' : 'sim-02';
          const t0 = performance.now();
          if (mode === 'b2b' && epoch > 1) dc.send(JSON.stringify({ t: 'unbind', slot: 0 }));
          dc.send(JSON.stringify({ t: 'bind', slot: 0, cam, profile: 'sub', epoch }));
          await sleep(playMs);
          const mine = presented.filter((frame) => frame.display >= t0 && frame.display < t0 + playMs);
          await sleep(Math.random() * jitterMs);
          const firstOwn = mine.findIndex((frame) => frame.received !== null && frame.received >= t0);
          const own = firstOwn < 0 ? [] : mine.slice(firstOwn);
          const first = mine[firstOwn];
          const after = await inbound();
          binds.push({
            epoch,
            cam,
            first:
              first === undefined || first.received === null
                ? null
                : { ms: round(first.display - t0), receivedMs: round(first.received - t0) },
            staleBeforeFirst: firstOwn < 0 ? mine.length : firstOwn,
            frames: own.length,
            holdMs: own.flatMap((frame) => (frame.received === null ? [] : [round(frame.display - frame.received)])),
            counters: Object.fromEntries(COUNTERS.map((key) => [key, (after[key] ?? 0) - (before[key] ?? 0)])),
          });
          before = after;
        }
        pc.close();
        return { binds, hubErrors };
      },
      { count: BINDS, mode, playMs: PLAY_MS, jitterMs: JITTER_MS },
    );

    const failed = binds.filter((bind) => {
      const firstOk = bind.first !== null && bind.first.receivedMs >= 0 && bind.first.ms <= LIMIT_MS;
      const hold = median(bind.holdMs);
      return !firstOk || hold === undefined || hold > LIMIT_MS;
    });
    const firsts = binds.flatMap((bind) => (bind.first === null ? [] : [bind.first.ms]));
    const holds = binds.flatMap((bind) => bind.holdMs);
    console.log(
      `e2e: re-binds ${mode}, jitter ${JITTER_MS} ms: ${binds.length - failed.length}/${binds.length} ok; ` +
        `first frame median ${median(firsts)} ms, max ${Math.max(...firsts)} ms; ` +
        `arrival to display median ${median(holds)} ms, max ${Math.max(...holds)} ms`,
    );

    expect(hubErrors, 'errors from the hub').toEqual([]);
    expect(binds).toHaveLength(BINDS);
    expect(failed, 'binds whose first frame was late or not their own, or whose frames were held').toEqual([]);
  });
}
