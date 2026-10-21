import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  OFFER_AFTER_GATHERING,
  OFFER_BEFORE_GATHERING,
  OFFER_WITH_A_CANDIDATE,
  stubPeerConnection,
  type FakePeerConnection,
  type FakePeerOptions,
} from './fakes';
import { BindError, ConnectError, openSession } from './session';

const HUB = 'http://192.168.1.20:7080';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const answer = (): Promise<Response> => Promise.resolve(new Response('v=0 answer', { status: 200 }));

/** Lets pending promise callbacks and zero-delay timers run (real timers only). */
const flush = (): Promise<void> => new Promise((resolve) => setTimeout(resolve, 0));

function stubFetch(handler: typeof fetch = answer) {
  const fetchMock = vi.fn<typeof fetch>(handler);
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

/** A fetch that never answers and fails with AbortError once its signal aborts, like the browser's. */
const silentHub: typeof fetch = (_url, init) =>
  new Promise((_resolve, reject) => {
    init?.signal?.addEventListener('abort', () => {
      reject(new DOMException('The operation was aborted.', 'AbortError'));
    });
  });

function pcOf(instances: FakePeerConnection[]): FakePeerConnection {
  const pc = instances[0];
  if (!pc) throw new Error('the app has not created a PeerConnection');
  return pc;
}

async function open(slots = 6, options: FakePeerOptions = {}) {
  const instances = stubPeerConnection(options);
  const fetchMock = stubFetch();
  const session = await openSession(HUB, slots);
  const pc = pcOf(instances);
  return { session, pc, rpc: pc.rpc, fetchMock };
}

describe('openSession', () => {
  it('openSession_offerHasSlotsPlusOneRecvonlyAndRpc', async () => {
    const { session, pc } = await open(6);

    expect(pc.addTransceiverCalls).toHaveLength(7);
    for (const call of pc.addTransceiverCalls) {
      expect(call).toEqual({ kind: 'video', init: { direction: 'recvonly' } });
    }
    expect(pc.channels.map((channel) => channel.label)).toEqual(['rpc']);
    // The slots are the video m-lines in order, whatever else the offer holds.
    expect(pc.calls).toEqual([...Array<string>(7).fill('addTransceiver'), 'createDataChannel:rpc']);
    expect(pc.config?.iceServers).toEqual([]); // LAN only: no STUN or TURN server is contacted
    expect(session.slots).toBe(6);
    expect(session.pc).toBe(pc.element);
  });

  it('openSession_postsTheOfferWithItsCandidatesAndAppliesTheAnswer', async () => {
    const instances = stubPeerConnection({ gathering: 'manual' });
    const fetchMock = stubFetch();

    const opening = openSession(`${HUB}/`, 2);
    await flush();
    expect(fetchMock).not.toHaveBeenCalled(); // the hub has no trickle: wait for the candidates

    pcOf(instances).finishGathering();
    await opening;

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe('http://192.168.1.20:7080/spike/v1/offer');
    expect(init?.method).toBe('POST');
    expect(init?.body).toBe(OFFER_AFTER_GATHERING);
    expect(pcOf(instances).remoteDescriptions).toEqual([{ type: 'answer', sdp: 'v=0 answer' }]);
  });

  it('openSession_gatheringTakesTooLong_postsWhatItHasAfter3s', async () => {
    vi.useFakeTimers();
    const instances = stubPeerConnection({ gathering: 'never' });
    const fetchMock = stubFetch();

    const opening = openSession(HUB, 1);
    await vi.advanceTimersByTimeAsync(2999);
    expect(fetchMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await opening;

    expect(fetchMock.mock.calls[0]?.[1]?.body).toBe(OFFER_BEFORE_GATHERING);
    expect(pcOf(instances).rpc.readyState).toBe('open');
  });

  it('openSession_candidatesStopComing_postsThemWithoutWaitingForComplete', async () => {
    // Chrome may not report gathering complete for seconds after its host candidates
    // (10 s in the batch C probe), and S5 times a reconnect (R44): the offer goes once
    // no candidate has come for 250 ms. Without STUN servers, host candidates are all there is.
    vi.useFakeTimers();
    const instances = stubPeerConnection({ gathering: 'never' });
    const fetchMock = stubFetch();

    const opening = openSession(HUB, 1);
    await vi.advanceTimersByTimeAsync(10);
    pcOf(instances).gatherCandidate();
    await vi.advanceTimersByTimeAsync(5);
    pcOf(instances).gatherCandidate(); // another interface, a few ms later
    await vi.advanceTimersByTimeAsync(249);
    expect(fetchMock).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    await opening;

    expect(fetchMock.mock.calls[0]?.[1]?.body).toBe(OFFER_WITH_A_CANDIDATE);
  });

  it('openSession_gatheringAlreadyComplete_doesNotWaitForAnEvent', async () => {
    // The state can be 'complete' by the time setLocalDescription resolves.
    const { fetchMock } = await open(1);

    expect(fetchMock.mock.calls[0]?.[1]?.body).toBe(OFFER_AFTER_GATHERING);
  });

  it('track_returnsTheReceiverTrackOfTheSlot', async () => {
    const { session } = await open(6);

    expect(session.track(0).id).toBe('track-0');
    expect(session.track(6).id).toBe('track-6'); // the extra slot
    expect(() => session.track(7)).toThrow(RangeError);
    expect(() => session.track(-1)).toThrow(RangeError);
  });
});

describe('openSession failures', () => {
  it('openSession_hubRefusesTheConnection_throwsConnectErrorRefusedWithTheUrl', async () => {
    const instances = stubPeerConnection();
    stubFetch(() => Promise.reject(new TypeError('Failed to fetch')));

    const failure = await openSession(HUB, 4).catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(ConnectError);
    expect(failure).toMatchObject({ name: 'ConnectError', kind: 'refused', url: HUB });
    expect(pcOf(instances).closeCalls).toBeGreaterThan(0); // nothing is left open
  });

  it('openSession_hubSilent_throwsConnectErrorTimeoutAfter10s', async () => {
    vi.useFakeTimers();
    const instances = stubPeerConnection();
    stubFetch(silentHub);

    const outcome = openSession(HUB, 4).catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(9999);
    const early = await Promise.race([outcome, Promise.resolve('pending')]);
    expect(early).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);

    expect(await outcome).toMatchObject({ name: 'ConnectError', kind: 'timeout', url: HUB });
    expect(pcOf(instances).closeCalls).toBeGreaterThan(0);
  });

  it('openSession_hubAnswersWithAnHttpError_throwsConnectErrorRejected', async () => {
    const instances = stubPeerConnection();
    stubFetch(() => Promise.resolve(new Response('origin not allowed', { status: 403 })));

    const failure = await openSession(HUB, 4).catch((error: unknown) => error);

    expect(failure).toMatchObject({ name: 'ConnectError', kind: 'rejected', url: HUB });
    expect((failure as Error).message).toContain('403');
    expect(pcOf(instances).closeCalls).toBeGreaterThan(0);
  });

  it('openSession_rpcChannelNeverOpens_throwsConnectErrorTimeoutAfter10s', async () => {
    // The offer was answered but UDP does not get through (a firewall on "Public", for example).
    vi.useFakeTimers();
    const instances = stubPeerConnection({ opensChannel: false });
    stubFetch();

    const outcome = openSession(HUB, 4).catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(9999);
    expect(await Promise.race([outcome, Promise.resolve('pending')])).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);

    expect(await outcome).toMatchObject({ name: 'ConnectError', kind: 'timeout', url: HUB });
    expect(pcOf(instances).closeCalls).toBeGreaterThan(0);
  });
});

describe('openSession, channel closed during the offer', () => {
  it('openSession_channelClosedDuringTheOffer_reportsRefusedAtOnce', async () => {
    // The channel gave up (the hub refused the association) while the offer was
    // still in flight: its close event has already fired, so waiting for it again
    // would end in a 10 s "timeout" that blames the network.
    vi.useFakeTimers();
    const instances = stubPeerConnection({ opensChannel: false });
    stubFetch(() => {
      pcOf(instances).rpc.close();
      return answer();
    });

    const outcome = openSession(HUB, 4).catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(0);

    expect(await Promise.race([outcome, Promise.resolve('pending')])).toMatchObject({
      name: 'ConnectError',
      kind: 'refused',
      url: HUB,
    });
  });
});

describe('bind', () => {
  it('bind_sendsBindAndResolvesOnBound', async () => {
    const { session, rpc } = await open();
    const before = performance.now();

    const pending = session.bind(3, 'sim-04', 'sub');
    const after = performance.now();

    expect(rpc.sent).toEqual([{ t: 'bind', slot: 3, cam: 'sim-04', profile: 'sub', epoch: expect.any(Number) }]);
    const epoch = Number(rpc.sent[0]?.epoch);
    rpc.receive({ t: 'bound', slot: 3, epoch, codec: 'H264', profile: '4d401f' });

    const result = await pending;
    expect(result).toStrictEqual({ epoch, codec: 'H264', requestedPerfMs: expect.any(Number) });
    // The time the bind was requested, on the performance clock, for the R26 filter in the scenarios.
    expect(result.requestedPerfMs).toBeGreaterThanOrEqual(before);
    expect(result.requestedPerfMs).toBeLessThanOrEqual(after);
  });

  it('bind_boundWithFallback_reportsTheProfileOnTheWire', async () => {
    const { session, rpc } = await open();

    const pending = session.bind(0, 'sim-01', 'sub');
    const epoch = Number(rpc.sent[0]?.epoch);
    rpc.receive({ t: 'bound', slot: 0, epoch, codec: 'H264', profile: '4d401f', fallback: '42e01f' });

    await expect(pending).resolves.toMatchObject({ epoch, codec: 'H264', fallback: '42e01f' });
  });

  it('bind_everyBindHasItsOwnIncreasingEpoch', async () => {
    const { session, rpc } = await open();

    void session.bind(0, 'sim-01', 'sub');
    void session.bind(0, 'sim-02', 'main');
    void session.bind(1, 'sim-01', 'sub');

    const epochs = rpc.sent.map((message) => Number(message.epoch));
    expect(new Set(epochs).size).toBe(3);
    expect(epochs).toEqual([...epochs].sort((a, b) => a - b));
  });

  it('bind_answersAreMatchedByEpochNotByOrder', async () => {
    const { session, rpc } = await open();
    const settled: string[] = [];
    const first = session.bind(0, 'sim-01', 'sub').then((r) => settled.push(`first:${r.codec}`));
    const second = session.bind(1, 'sim-02', 'sub').then((r) => settled.push(`second:${r.codec}`));
    const [e1, e2] = rpc.sent.map((message) => Number(message.epoch));

    rpc.receive({ t: 'bound', slot: 1, epoch: e2, codec: 'H265', profile: '017b' });
    await flush();
    expect(settled).toEqual(['second:H265']);
    rpc.receive({ t: 'bound', slot: 0, epoch: e1, codec: 'H264', profile: '4d401f' });
    await Promise.all([first, second]);

    expect(settled).toEqual(['second:H265', 'first:H264']);
  });

  it('bind_boundOfAnotherEpoch_leavesTheBindPending', async () => {
    const { session, rpc } = await open();
    let settled = false;
    const pending = session.bind(0, 'sim-01', 'sub').finally(() => {
      settled = true;
    });
    const epoch = Number(rpc.sent[0]?.epoch);

    rpc.receive({ t: 'bound', slot: 0, epoch: epoch + 100, codec: 'H264', profile: '4d401f' });
    await flush();
    expect(settled).toBe(false);

    rpc.receive({ t: 'bound', slot: 0, epoch, codec: 'H264', profile: '4d401f' });
    await pending;
    expect(settled).toBe(true);
  });

  it.each(['CODEC_UNSUPPORTED', 'UNKNOWN_CAMERA', 'BAD_SLOT'])('bind_hubError%s_rejectsWithTheCode', async (code) => {
    const { session, rpc } = await open();

    const pending = session.bind(2, 'mx-h265', 'main');
    const epoch = Number(rpc.sent[0]?.epoch);
    rpc.receive({ t: 'error', slot: 2, epoch, code });

    const failure = await pending.catch((error: unknown) => error);
    expect(failure).toBeInstanceOf(BindError);
    expect(failure).toMatchObject({ name: 'BindError', code, message: code });
  });

  it('bind_failedBind_doesNotStopTheNextOne', async () => {
    const { session, rpc } = await open();
    const failed = session.bind(0, 'mx-h265', 'main');
    rpc.receive({ t: 'error', slot: 0, epoch: Number(rpc.sent[0]?.epoch), code: 'CODEC_UNSUPPORTED' });
    await expect(failed).rejects.toMatchObject({ code: 'CODEC_UNSUPPORTED' });

    const retry = session.bind(0, 'sim-01', 'sub');
    rpc.receive({ t: 'bound', slot: 0, epoch: Number(rpc.sent[1]?.epoch), codec: 'H264', profile: '4d401f' });

    await expect(retry).resolves.toMatchObject({ codec: 'H264' });
  });

  it('bind_noAnswerWithin5s_rejectsWithTimeout', async () => {
    // A bind sent during a Wi-Fi drop must not hang until SCTP gives up.
    vi.useFakeTimers();
    const { session, rpc } = await open();

    const outcome = session.bind(0, 'sim-01', 'sub').catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(4999);
    expect(await Promise.race([outcome, Promise.resolve('pending')])).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);

    const failure = await outcome;
    expect(failure).toBeInstanceOf(BindError);
    expect(failure).toMatchObject({ code: 'TIMEOUT' });
    // A late answer is ignored, and the next bind works.
    rpc.receive({ t: 'bound', slot: 0, epoch: Number(rpc.sent[0]?.epoch), codec: 'H264', profile: '4d401f' });
    const next = session.bind(0, 'sim-01', 'sub');
    rpc.receive({ t: 'bound', slot: 0, epoch: Number(rpc.sent[1]?.epoch), codec: 'H264', profile: '4d401f' });
    await expect(next).resolves.toMatchObject({ codec: 'H264' });
  });

  it('bind_answeredInTime_leavesNoTimerBehind', async () => {
    vi.useFakeTimers();
    const { session, rpc } = await open();

    const pending = session.bind(0, 'sim-01', 'sub');
    rpc.receive({ t: 'bound', slot: 0, epoch: Number(rpc.sent[0]?.epoch), codec: 'H264', profile: '4d401f' });
    await pending;

    expect(vi.getTimerCount()).toBe(0);
  });

  it('unbind_closingChannel_isANoOp', async () => {
    // Teardown in a scenario's finally block runs while the channel may be closing.
    const { session, rpc } = await open();
    for (const state of ['closing', 'closed'] as const) {
      rpc.readyState = state;

      expect(() => {
        session.unbind(0);
      }).not.toThrow();
    }
    expect(rpc.sent).toEqual([]);
  });

  it('unbind_sendsUnbindAndDoesNotWait', async () => {
    const { session, rpc } = await open();

    expect(session.unbind(2)).toBeUndefined();

    expect(rpc.sent).toEqual([{ t: 'unbind', slot: 2 }]);
  });
});

describe('messages from the hub', () => {
  it('onSent_deliversCamProfileAndRecords', async () => {
    const { session, rpc } = await open();
    const received: unknown[] = [];
    session.onSent((cam, profile, recs) => received.push([cam, profile, recs]));

    rpc.receive({ t: 'sent', cam: 4, p: 'sub', r: [[10, 0, 1_729_260_000_100.5], [11, 0, 1_729_260_000_183.25]] });
    rpc.receive({ t: 'sent', cam: 4, p: 'main', r: [[3, 1, 1_729_260_000_200]] });

    expect(received).toEqual([
      [4, 'sub', [[10, 0, 1_729_260_000_100.5], [11, 0, 1_729_260_000_183.25]]],
      [4, 'main', [[3, 1, 1_729_260_000_200]]],
    ]);
  });

  it('onSent_unsubscribe_stopsDelivery', async () => {
    const { session, rpc } = await open();
    const received: number[] = [];
    const stop = session.onSent((cam) => received.push(cam));
    rpc.receive({ t: 'sent', cam: 1, p: 'sub', r: [] });

    stop();
    rpc.receive({ t: 'sent', cam: 2, p: 'sub', r: [] });

    expect(received).toEqual([1]);
  });

  it('onSent_malformedMessages_areIgnored', async () => {
    const { session, rpc } = await open();
    const received: number[] = [];
    session.onSent((cam) => received.push(cam));

    rpc.receive('not json');
    rpc.receive('[1,2,3]');
    rpc.receive({ t: 'unknown' });
    rpc.receive({ t: 'sent', cam: 'x', p: 'sub', r: [] });
    rpc.receive({ t: 'sent', cam: 1, p: 'huge', r: [] });
    rpc.receive({ t: 'sent', cam: 1, p: 'sub', r: [[1, 2]] });
    rpc.receive({ t: 'sent', cam: 1, p: 'sub', r: 'no' });
    rpc.receive({ t: 'sent', cam: 9, p: 'sub', r: [] });

    expect(received).toEqual([9]);
  });

  it('pair_resolvesOnTheFirstPairMessage', async () => {
    const { session, rpc } = await open();
    const pending = session.pair();

    rpc.receive({ t: 'pair', local: 'host', remote: 'prflx' });

    await expect(pending).resolves.toEqual({ local: 'host', remote: 'prflx' });
  });

  it('pair_afterThePairMessage_resolvesAtOnceWithTheLatest', async () => {
    const { session, rpc } = await open();
    rpc.receive({ t: 'pair', local: 'host', remote: 'prflx' });
    rpc.receive({ t: 'pair', local: 'host', remote: 'host' }); // ICE selected another pair

    await expect(session.pair()).resolves.toEqual({ local: 'host', remote: 'host' });
  });

  it('pair_noPairWithin10s_resolvesNa', async () => {
    // A hub whose ICE never reports a selected pair must not hang S10.
    vi.useFakeTimers();
    const { session, rpc } = await open();

    const pending = session.pair();
    await vi.advanceTimersByTimeAsync(9999);
    expect(await Promise.race([pending, Promise.resolve('pending')])).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);

    await expect(pending).resolves.toBe('n/a');
    // A pair that comes later is still reported to the next caller.
    rpc.receive({ t: 'pair', local: 'host', remote: 'host' });
    await expect(session.pair()).resolves.toEqual({ local: 'host', remote: 'host' });
  });

  it('pair_timeoutOfAWaiterThatIsGone_removesNoOtherWaiter', async () => {
    // A drain that took a waiter off the list without clearing its timer (none
    // does today) must not make that timer remove another waiter.
    vi.useFakeTimers();
    const { session, rpc } = await open();
    void session.pair(); // waiter A, times out at 10 s
    await vi.advanceTimersByTimeAsync(1000);
    const later = session.pair(); // waiter B, times out at 11 s
    (session as unknown as { pairWaiters: unknown[] }).pairWaiters.shift(); // A leaves, its timer stays

    await vi.advanceTimersByTimeAsync(9500); // A's timer fires
    rpc.receive({ t: 'pair', local: 'host', remote: 'prflx' });
    await vi.advanceTimersByTimeAsync(1000); // past B's own timeout

    await expect(later).resolves.toEqual({ local: 'host', remote: 'prflx' });
  });

  it('onJitter_deliversEachReportInMs', async () => {
    const { session, rpc } = await open();
    const reports: number[] = [];
    const stop = session.onJitter((ms) => reports.push(ms));

    rpc.receive({ t: 'jitter', ms: 3.1 });
    rpc.receive({ t: 'jitter', ms: 22.5 });
    rpc.receive({ t: 'jitter', ms: 'fast' });
    stop();
    rpc.receive({ t: 'jitter', ms: 1 });

    expect(reports).toEqual([3.1, 22.5]);
  });
});

describe('ping', () => {
  it('ping_resolvesWithTheClientTimesAndTheHubTime', async () => {
    const { session, rpc } = await open();
    const before = performance.timeOrigin + performance.now();

    const pending = session.ping();

    const message = rpc.sent[0];
    expect(message).toEqual({ t: 'ping', c0: expect.any(Number) });
    const c0 = Number(message?.c0);
    rpc.receive({ t: 'pong', c0, h: 1_729_260_000_555.5 });
    const sample = await pending;
    const after = performance.timeOrigin + performance.now();

    // Unix-scale times from the performance clock, never from Date.now(): a wrong device clock only shifts the origin.
    expect(sample).toEqual({ c0, c1: expect.any(Number), h: 1_729_260_000_555.5 });
    expect(c0).toBeGreaterThanOrEqual(before);
    expect(sample.c1).toBeGreaterThanOrEqual(c0);
    expect(sample.c1).toBeLessThanOrEqual(after);
  });

  it('ping_pongOfAnotherPing_isIgnored', async () => {
    const { session, rpc } = await open();
    let settled = false;
    const pending = session.ping().finally(() => {
      settled = true;
    });
    const c0 = Number(rpc.sent[0]?.c0);

    rpc.receive({ t: 'pong', c0: c0 + 1, h: 5 });
    await flush();
    expect(settled).toBe(false);

    rpc.receive({ t: 'pong', c0, h: 6 });
    await expect(pending).resolves.toMatchObject({ h: 6 });
  });

  it('ping_noPong_rejectsAfter2s', async () => {
    vi.useFakeTimers();
    const { session } = await open();

    const outcome = session.ping().catch((error: unknown) => error);
    await vi.advanceTimersByTimeAsync(1999);
    expect(await Promise.race([outcome, Promise.resolve('pending')])).toBe('pending');
    await vi.advanceTimersByTimeAsync(1);

    expect(await outcome).toMatchObject({ message: 'ping timed out' });
  });
});

describe('close', () => {
  it('close_rejectsWhatIsPendingAndClosesTheConnection', async () => {
    const { session, pc } = await open();
    const bind = session.bind(0, 'sim-01', 'sub');
    const ping = session.ping();
    const pair = session.pair();

    session.close();

    for (const pending of [bind, ping, pair]) {
      await expect(pending).rejects.toThrow('SESSION_CLOSED');
    }
    expect(pc.closeCalls).toBe(1);
    session.close(); // safe to repeat
    expect(pc.closeCalls).toBe(1);
  });

  it('bind_afterClose_rejects', async () => {
    const { session, rpc } = await open();
    session.close();

    await expect(session.bind(0, 'sim-01', 'sub')).rejects.toThrow('SESSION_CLOSED');
    await expect(session.ping()).rejects.toThrow('SESSION_CLOSED');
    expect(rpc.sent).toEqual([]);
  });

  it('unbind_afterClose_isANoOp', async () => {
    const { session } = await open();
    session.close();

    expect(() => {
      session.unbind(0);
    }).not.toThrow();
  });

  it('rpcChannelClosedByTheHub_rejectsPendingBinds', async () => {
    // The hub went away or the Wi-Fi dropped: a bind must not hang forever.
    const { session, rpc } = await open();
    const bind = session.bind(0, 'sim-01', 'sub');

    rpc.close();

    await expect(bind).rejects.toThrow('SESSION_CLOSED');
    await expect(session.bind(1, 'sim-01', 'sub')).rejects.toThrow('SESSION_CLOSED');
  });
});

describe('connection state', () => {
  it('connectionState_followsThePeerConnectionAndEndsClosed', async () => {
    // R49: the runner ends a run as interrupted when the hub session is lost, so it must see it.
    const { session, pc } = await open();
    const seen: string[] = [];
    session.onConnectionState((state) => seen.push(state));
    pc.setConnectionState('connected');

    expect(session.connectionState()).toBe('connected');
    pc.setConnectionState('disconnected');
    pc.setConnectionState('connected');
    pc.setConnectionState('failed');

    expect(seen).toEqual(['connected', 'disconnected', 'connected', 'failed']);
    expect(session.connectionState()).toBe('failed');
  });

  it('connectionState_rpcChannelClosedByTheHub_isClosed', async () => {
    // The hub went away: the browser fires no connectionstatechange for the close that follows.
    const { session, pc, rpc } = await open();
    pc.setConnectionState('connected');
    const seen: string[] = [];
    const stop = session.onConnectionState((state) => seen.push(state));

    rpc.close();

    expect(session.connectionState()).toBe('closed');
    expect(seen).toEqual(['closed']);
    stop();
  });

  it('connectionState_closedByTheApp_isReportedOnce', async () => {
    const { session, pc } = await open();
    pc.setConnectionState('connected');
    const seen: string[] = [];
    const stop = session.onConnectionState((state) => seen.push(state));
    session.close();
    session.close();
    stop();
    pc.setConnectionState('closed');

    expect(seen).toEqual(['closed']);
  });
});
