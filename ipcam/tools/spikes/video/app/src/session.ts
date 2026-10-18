// The spike app's side of the hub protocol: one PeerConnection with N+1 recvonly
// video slots and an rpc data channel. The message shapes are documented in the
// package doc of tools/spikes/video/session (session.go), which is the source of
// truth for them.
import type { PingSample } from './clock';
import { ConnectError, requestHub } from './hubhttp';

export { ConnectError, type ConnectFailure } from './hubhttp';

export type Profile = 'sub' | 'main';

/** The candidate types of an ICE pair, `host`, `srflx`, `prflx` or `relay`. */
export interface Pair {
  local: string;
  remote: string;
}

export interface BindResult {
  epoch: number;
  /** The codec the hub sends: `H264` or `H265`. */
  codec: string;
  /**
   * Set only when a `sub` stream goes out in another H.264 profile-level-id than
   * its own, because the offer lacks its profile (ruling R23): that id.
   */
  fallback?: string;
  /**
   * `performance.now()` when `bind` was called. Sent records whose hub time
   * precedes it belong to the GOP the hub had cached, not to the bind, and are
   * not latency samples (R26): `predatesBind` (clock.ts) says which, given this
   * value and the `ClockSync` offset.
   */
  requestedPerfMs: number;
}

export interface SpikeSession {
  /**
   * The number of tile slots asked for. The offer holds one more m-line: slot
   * `slots` is the extra one, for the fullscreen `main` stream. Valid slots are
   * 0 to `slots`, in m-line order, as the hub numbers them.
   */
  readonly slots: number;
  readonly pc: RTCPeerConnection;
  /**
   * Binds the camera `cam` (a hub camera id such as `sim-04`) to a slot. Resolves
   * at the hub's `bound`; rejects with a `BindError` carrying the hub's code, or
   * `TIMEOUT` when neither `bound` nor `error` came within 5 s (a late answer is
   * ignored). Rejects with `SESSION_CLOSED` when the session ends first.
   */
  bind(slot: number, cam: string, profile: Profile): Promise<BindResult>;
  /** The hub does not answer an unbind. Does nothing once the channel is closing or closed, so it is safe in teardown. */
  unbind(slot: number): void;
  /** The track that a slot receives. Throws a RangeError for a slot that does not exist. */
  track(slot: number): MediaStreamTrack;
  /**
   * Frames that simulated cameras sent: `cam` is the simulated camera number and
   * `profile` the stream of the bind (ruling R3); each record is
   * `[frame, loop, hubMs]`, `hubMs` being the hub's Unix time in ms. Returns the
   * function that stops the callback.
   */
  onSent(cb: (cam: number, profile: Profile, recs: [number, number, number][]) => void): () => void;
  /** The hub's pacing jitter in ms, one report per 10 s while a camera is bound (R28). Returns the function that stops the callback. */
  onJitter(cb: (ms: number) => void): () => void;
  /**
   * The candidate types of the selected ICE pair, never addresses. Resolves at
   * the first `pair` message, then with the latest one; `n/a` when none came
   * within 10 s.
   */
  pair(): Promise<Pair | 'n/a'>;
  /** One clock ping, for `ClockSync`. Rejects after 2 s without an answer. Pings must not overlap. */
  ping(): Promise<PingSample>;
  /** Idempotent. Rejects whatever is pending with `SESSION_CLOSED`. */
  close(): void;
}

/**
 * The hub refused a bind. `code` is its error code, `CODEC_UNSUPPORTED`,
 * `UNKNOWN_CAMERA` or `BAD_SLOT`, or `TIMEOUT` when the hub did not answer in
 * time; the message is the code.
 */
export class BindError extends Error {
  readonly code: string;

  constructor(code: string) {
    super(code);
    this.name = 'BindError';
    this.code = code;
  }
}

/** How long the offer waits for ICE gathering, which on a LAN without STUN servers only lists the host's own addresses. */
const GATHER_TIMEOUT_MS = 3000;
const OFFER_TIMEOUT_MS = 10_000;
const RPC_OPEN_TIMEOUT_MS = 10_000;
const PING_TIMEOUT_MS = 2000;
const BIND_TIMEOUT_MS = 5000;
const PAIR_TIMEOUT_MS = 10_000;
const SESSION_CLOSED = 'SESSION_CLOSED';

/** Unix milliseconds on the performance clock: monotonic, and the scale `ClockSync` and `glassToGlassMs` use. */
const clientNowMs = (): number => performance.timeOrigin + performance.now();

const isRecord = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null;

type HubMessage =
  | { t: 'bound'; epoch: number; codec: string; fallback?: string }
  | { t: 'error'; epoch: number; code: string }
  | { t: 'pong'; c0: number; h: number }
  | { t: 'sent'; cam: number; p: Profile; r: [number, number, number][] }
  | { t: 'pair'; local: string; remote: string }
  | { t: 'jitter'; ms: number };

function parseRecords(value: unknown): [number, number, number][] | undefined {
  if (!Array.isArray(value)) return undefined;
  const records: [number, number, number][] = [];
  for (const entry of value as unknown[]) {
    if (!Array.isArray(entry) || entry.length !== 3) return undefined;
    const [frame, loop, hubMs] = entry as unknown[];
    if (typeof frame !== 'number' || typeof loop !== 'number' || typeof hubMs !== 'number') return undefined;
    records.push([frame, loop, hubMs]);
  }
  return records;
}

/** Decodes one message of the hub. Undefined for anything that is not one of the six shapes. */
function parseHubMessage(data: unknown): HubMessage | undefined {
  if (typeof data !== 'string') return undefined;
  let msg: unknown;
  try {
    msg = JSON.parse(data);
  } catch {
    return undefined;
  }
  if (!isRecord(msg)) return undefined;
  switch (msg.t) {
    case 'bound':
      if (typeof msg.epoch !== 'number' || typeof msg.codec !== 'string') return undefined;
      return {
        t: 'bound',
        epoch: msg.epoch,
        codec: msg.codec,
        ...(typeof msg.fallback === 'string' && msg.fallback !== '' ? { fallback: msg.fallback } : {}),
      };
    case 'error':
      if (typeof msg.epoch !== 'number' || typeof msg.code !== 'string') return undefined;
      return { t: 'error', epoch: msg.epoch, code: msg.code };
    case 'pong':
      if (typeof msg.c0 !== 'number' || typeof msg.h !== 'number') return undefined;
      return { t: 'pong', c0: msg.c0, h: msg.h };
    case 'sent': {
      const records = parseRecords(msg.r);
      if (typeof msg.cam !== 'number' || (msg.p !== 'sub' && msg.p !== 'main') || records === undefined) return undefined;
      return { t: 'sent', cam: msg.cam, p: msg.p, r: records };
    }
    case 'pair':
      if (typeof msg.local !== 'string' || typeof msg.remote !== 'string') return undefined;
      return { t: 'pair', local: msg.local, remote: msg.remote };
    case 'jitter':
      if (typeof msg.ms !== 'number') return undefined;
      return { t: 'jitter', ms: msg.ms };
    default:
      return undefined;
  }
}

interface Pending<T> {
  resolve: (value: T) => void;
  reject: (reason: Error) => void;
}

interface PendingPing extends Pending<number> {
  timer: ReturnType<typeof setTimeout>;
}

class HubSession implements SpikeSession {
  readonly slots: number;
  readonly pc: RTCPeerConnection;
  private readonly transceivers: RTCRtpTransceiver[];
  private readonly rpc: RTCDataChannel;
  private closed = false;
  private nextEpoch = 1;
  private readonly binds = new Map<number, Pending<Omit<BindResult, 'requestedPerfMs'>>>();
  private readonly pings = new Map<number, PendingPing>();
  private readonly pairWaiters: Pending<Pair>[] = [];
  private latestPair: Pair | undefined;
  private readonly sentListeners = new Set<(cam: number, profile: Profile, recs: [number, number, number][]) => void>();
  private readonly jitterListeners = new Set<(ms: number) => void>();

  constructor(slots: number, pc: RTCPeerConnection, transceivers: RTCRtpTransceiver[], rpc: RTCDataChannel) {
    this.slots = slots;
    this.pc = pc;
    this.transceivers = transceivers;
    this.rpc = rpc;
    // From here on, so that nothing the hub sends right after the channel opens is missed.
    rpc.addEventListener('message', (event) => {
      this.onMessage((event as MessageEvent<unknown>).data);
    });
    rpc.addEventListener('close', () => {
      this.close();
    });
  }

  bind(slot: number, cam: string, profile: Profile): Promise<BindResult> {
    if (this.closed) return Promise.reject(new Error(SESSION_CLOSED));
    const requestedPerfMs = performance.now();
    const epoch = this.nextEpoch++;
    return new Promise<BindResult>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.binds.delete(epoch);
        reject(new BindError('TIMEOUT'));
      }, BIND_TIMEOUT_MS);
      this.binds.set(epoch, {
        resolve: (bound) => {
          clearTimeout(timer);
          resolve({ ...bound, requestedPerfMs });
        },
        reject: (reason) => {
          clearTimeout(timer);
          reject(reason);
        },
      });
      try {
        this.send({ t: 'bind', slot, cam, profile, epoch });
      } catch (error) {
        clearTimeout(timer);
        this.binds.delete(epoch);
        reject(error as Error);
      }
    });
  }

  unbind(slot: number): void {
    if (this.closed || this.rpc.readyState !== 'open') return;
    this.send({ t: 'unbind', slot });
  }

  track(slot: number): MediaStreamTrack {
    const transceiver = this.transceivers[slot];
    if (transceiver === undefined) throw new RangeError(`slot ${slot} does not exist (0 to ${this.slots})`);
    return transceiver.receiver.track;
  }

  onSent(cb: (cam: number, profile: Profile, recs: [number, number, number][]) => void): () => void {
    this.sentListeners.add(cb);
    return () => {
      this.sentListeners.delete(cb);
    };
  }

  onJitter(cb: (ms: number) => void): () => void {
    this.jitterListeners.add(cb);
    return () => {
      this.jitterListeners.delete(cb);
    };
  }

  pair(): Promise<Pair | 'n/a'> {
    if (this.latestPair) return Promise.resolve(this.latestPair);
    if (this.closed) return Promise.reject(new Error(SESSION_CLOSED));
    return new Promise<Pair | 'n/a'>((resolve, reject) => {
      const waiter: Pending<Pair> = {
        resolve: (pair) => {
          clearTimeout(timer);
          resolve(pair);
        },
        reject: (reason) => {
          clearTimeout(timer);
          reject(reason);
        },
      };
      const timer = setTimeout(() => {
        this.pairWaiters.splice(this.pairWaiters.indexOf(waiter), 1);
        resolve('n/a');
      }, PAIR_TIMEOUT_MS);
      this.pairWaiters.push(waiter);
    });
  }

  ping(): Promise<PingSample> {
    if (this.closed) return Promise.reject(new Error(SESSION_CLOSED));
    const c0 = clientNowMs();
    return new Promise<PingSample>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pings.delete(c0);
        reject(new Error('ping timed out'));
      }, PING_TIMEOUT_MS);
      this.pings.set(c0, { resolve: (h) => resolve({ c0, c1: clientNowMs(), h }), reject, timer });
      try {
        this.send({ t: 'ping', c0 });
      } catch (error) {
        clearTimeout(timer);
        this.pings.delete(c0);
        reject(error as Error);
      }
    });
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    const reason = new Error(SESSION_CLOSED);
    for (const pending of this.binds.values()) pending.reject(reason);
    this.binds.clear();
    for (const pending of this.pings.values()) {
      clearTimeout(pending.timer);
      pending.reject(reason);
    }
    this.pings.clear();
    for (const waiter of this.pairWaiters.splice(0)) waiter.reject(reason);
    this.pc.close();
  }

  private send(message: object): void {
    this.rpc.send(JSON.stringify(message));
  }

  private onMessage(data: unknown): void {
    const msg = parseHubMessage(data);
    if (msg === undefined) return;
    switch (msg.t) {
      case 'bound': {
        const pending = this.binds.get(msg.epoch);
        if (pending === undefined) return;
        this.binds.delete(msg.epoch);
        pending.resolve({
          epoch: msg.epoch,
          codec: msg.codec,
          ...(msg.fallback === undefined ? {} : { fallback: msg.fallback }),
        });
        return;
      }
      case 'error': {
        const pending = this.binds.get(msg.epoch);
        if (pending === undefined) return;
        this.binds.delete(msg.epoch);
        pending.reject(new BindError(msg.code));
        return;
      }
      case 'pong': {
        const pending = this.pings.get(msg.c0);
        if (pending === undefined) return;
        this.pings.delete(msg.c0);
        clearTimeout(pending.timer);
        pending.resolve(msg.h);
        return;
      }
      case 'sent':
        for (const cb of [...this.sentListeners]) cb(msg.cam, msg.p, msg.r);
        return;
      case 'pair':
        this.latestPair = { local: msg.local, remote: msg.remote };
        for (const waiter of this.pairWaiters.splice(0)) waiter.resolve(this.latestPair);
        return;
      case 'jitter':
        for (const cb of [...this.jitterListeners]) cb(msg.ms);
        return;
    }
  }
}

/** Resolves when ICE gathering is complete, or after GATHER_TIMEOUT_MS with the candidates found so far. */
function iceGathered(pc: RTCPeerConnection): Promise<void> {
  if (pc.iceGatheringState === 'complete') return Promise.resolve();
  return new Promise<void>((resolve) => {
    const finish = (): void => {
      clearTimeout(timer);
      pc.removeEventListener('icegatheringstatechange', onChange);
      resolve();
    };
    const onChange = (): void => {
      if (pc.iceGatheringState === 'complete') finish();
    };
    const timer = setTimeout(finish, GATHER_TIMEOUT_MS);
    pc.addEventListener('icegatheringstatechange', onChange);
  });
}

// SPIKE-ONLY (phase0b video spike plan, Global Constraints): the offer goes to the
// hub over plain HTTP on the LAN, port 7080, so that a phone can reach it without
// a certificate. Production signalling does not. The body is text/plain, which
// needs no CORS preflight from the shells' origins.
function postOffer(hubUrl: string, sdp: string): Promise<string> {
  return requestHub(
    hubUrl,
    '/spike/v1/offer',
    { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: sdp },
    (response) => response.text(),
    OFFER_TIMEOUT_MS,
  );
}

/**
 * Resolves when the channel is open. Rejects with a ConnectError when it closes
 * first, or has closed already (its close event fired during the offer), or
 * stays closed for RPC_OPEN_TIMEOUT_MS.
 */
function rpcOpen(rpc: RTCDataChannel, hubUrl: string): Promise<void> {
  if (rpc.readyState === 'open') return Promise.resolve();
  if (rpc.readyState === 'closing' || rpc.readyState === 'closed') {
    return Promise.reject(new ConnectError('refused', hubUrl, 'the rpc channel closed before it opened'));
  }
  return new Promise<void>((resolve, reject) => {
    const settle = (): void => {
      clearTimeout(timer);
      rpc.removeEventListener('open', onOpen);
      rpc.removeEventListener('close', onClose);
    };
    const onOpen = (): void => {
      settle();
      resolve();
    };
    const onClose = (): void => {
      settle();
      reject(new ConnectError('refused', hubUrl, 'the rpc channel closed before it opened'));
    };
    const timer = setTimeout(() => {
      settle();
      reject(new ConnectError('timeout', hubUrl, `the rpc channel did not open within ${RPC_OPEN_TIMEOUT_MS / 1000} s`));
    }, RPC_OPEN_TIMEOUT_MS);
    rpc.addEventListener('open', onOpen);
    rpc.addEventListener('close', onClose);
  });
}

/**
 * Connects to the hub at `hubUrl` (for example `http://192.168.1.20:7080`): offers
 * `slots + 1` recvonly video transceivers and the data channel `rpc`, sends the
 * offer once ICE gathering is done (the hub takes no trickle), and resolves when
 * the channel is open. Rejects with a `ConnectError`; the PeerConnection is
 * closed then. No ICE server is configured: the spike runs on the LAN only.
 */
export async function openSession(hubUrl: string, slots: number): Promise<SpikeSession> {
  const url = hubUrl.replace(/\/+$/, '');
  const pc = new RTCPeerConnection({ iceServers: [] });
  // The slots are the video m-lines in order, so the channel's m-line comes after them.
  const transceivers: RTCRtpTransceiver[] = [];
  for (let i = 0; i <= slots; i += 1) transceivers.push(pc.addTransceiver('video', { direction: 'recvonly' }));
  const rpc = pc.createDataChannel('rpc');
  const session = new HubSession(slots, pc, transceivers, rpc);
  try {
    await pc.setLocalDescription(await pc.createOffer());
    await iceGathered(pc);
    const offer = pc.localDescription?.sdp;
    if (offer === undefined) throw new Error('the PeerConnection has no local description');
    await pc.setRemoteDescription({ type: 'answer', sdp: await postOffer(url, offer) });
    await rpcOpen(rpc, url);
  } catch (error) {
    session.close();
    throw error;
  }
  return session;
}
