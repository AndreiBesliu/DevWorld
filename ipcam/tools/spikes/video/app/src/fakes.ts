// Test doubles for the session tests. Not part of the shipped app.
import { vi } from 'vitest';

export interface FakeTrack {
  id: string;
  kind: string;
}

export interface FakeTransceiver {
  mid: string;
  direction: string | undefined;
  receiver: { track: FakeTrack };
}

/** A message the app sent over the rpc channel, decoded. */
export type Sent = { t: string } & Record<string, unknown>;

/** The rpc channel as the browser exposes it, plus what the hub side does with it. */
export class FakeDataChannel extends EventTarget {
  readonly label: string;
  readyState: RTCDataChannelState = 'connecting';
  /** What the app sent, as the JSON text it sent. */
  readonly sentText: string[] = [];

  constructor(label: string) {
    super();
    this.label = label;
  }

  get element(): RTCDataChannel {
    return this as unknown as RTCDataChannel;
  }

  get sent(): Sent[] {
    return this.sentText.map((text) => JSON.parse(text) as Sent);
  }

  send(data: string): void {
    if (this.readyState !== 'open') throw new Error('InvalidStateError: the channel is not open');
    this.sentText.push(data);
  }

  close(): void {
    if (this.readyState === 'closed') return;
    this.readyState = 'closed';
    this.dispatchEvent(new Event('close'));
  }

  /** The hub side finishes the channel's set-up. */
  open(): void {
    this.readyState = 'open';
    this.dispatchEvent(new Event('open'));
  }

  /** The hub says something: an object is sent as JSON, a string as it is. */
  receive(message: object | string): void {
    const data = typeof message === 'string' ? message : JSON.stringify(message);
    this.dispatchEvent(new MessageEvent('message', { data }));
  }
}

export interface FakePeerOptions {
  /**
   * When ICE gathering completes. `auto`: right after setLocalDescription.
   * `manual`: when the test calls `finishGathering()`. `never`: it does not.
   */
  gathering?: 'auto' | 'manual' | 'never';
  /** Whether the rpc channel opens once the answer is applied. Default: true. */
  opensChannel?: boolean;
}

export const OFFER_BEFORE_GATHERING = 'v=0 offer without candidates';
export const OFFER_AFTER_GATHERING = 'v=0 offer with candidates';

/** The parts of RTCPeerConnection that openSession uses. */
export class FakePeerConnection extends EventTarget {
  readonly config: RTCConfiguration | undefined;
  readonly transceivers: FakeTransceiver[] = [];
  readonly addTransceiverCalls: { kind: string; init: RTCRtpTransceiverInit | undefined }[] = [];
  readonly channels: FakeDataChannel[] = [];
  /** Interleaves addTransceiver and createDataChannel calls, to check the m-line order. */
  readonly calls: string[] = [];
  readonly remoteDescriptions: RTCSessionDescriptionInit[] = [];
  localDescription: RTCSessionDescriptionInit | null = null;
  iceGatheringState: RTCIceGatheringState = 'new';
  connectionState: RTCPeerConnectionState = 'new';
  closeCalls = 0;
  private readonly options: FakePeerOptions;

  constructor(options: FakePeerOptions, config?: RTCConfiguration) {
    super();
    this.options = options;
    this.config = config;
  }

  get element(): RTCPeerConnection {
    return this as unknown as RTCPeerConnection;
  }

  /** The channel that the app created with createDataChannel('rpc'). */
  get rpc(): FakeDataChannel {
    const channel = this.channels.find((c) => c.label === 'rpc');
    if (!channel) throw new Error('the app has not created the rpc channel');
    return channel;
  }

  addTransceiver(kind: string, init?: RTCRtpTransceiverInit): FakeTransceiver {
    const index = this.transceivers.length;
    const transceiver: FakeTransceiver = {
      mid: String(index),
      direction: init?.direction,
      receiver: { track: { id: `track-${index}`, kind } },
    };
    this.transceivers.push(transceiver);
    this.addTransceiverCalls.push({ kind, init });
    this.calls.push('addTransceiver');
    return transceiver;
  }

  getTransceivers(): FakeTransceiver[] {
    return this.transceivers;
  }

  createDataChannel(label: string): RTCDataChannel {
    const channel = new FakeDataChannel(label);
    this.channels.push(channel);
    this.calls.push(`createDataChannel:${label}`);
    return channel.element;
  }

  createOffer(): Promise<RTCSessionDescriptionInit> {
    return Promise.resolve({ type: 'offer', sdp: OFFER_BEFORE_GATHERING });
  }

  setLocalDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.localDescription = { type: description.type ?? 'offer', sdp: description.sdp ?? '' };
    this.iceGatheringState = 'gathering';
    if ((this.options.gathering ?? 'auto') === 'auto') queueMicrotask(() => this.finishGathering());
    return Promise.resolve();
  }

  /** ICE gathering is complete: the local description now carries the candidates. */
  finishGathering(): void {
    this.localDescription = { type: 'offer', sdp: OFFER_AFTER_GATHERING };
    this.iceGatheringState = 'complete';
    this.dispatchEvent(new Event('icegatheringstatechange'));
  }

  setRemoteDescription(description: RTCSessionDescriptionInit): Promise<void> {
    this.remoteDescriptions.push(description);
    if (this.options.opensChannel ?? true) queueMicrotask(() => this.rpc.open());
    return Promise.resolve();
  }

  close(): void {
    this.closeCalls += 1;
    this.connectionState = 'closed';
    for (const channel of this.channels) channel.close();
  }
}

/**
 * Replaces the global RTCPeerConnection with the fake. Returns the list that
 * collects every connection the app creates. Undone by `vi.unstubAllGlobals()`.
 */
export function stubPeerConnection(options: FakePeerOptions = {}): FakePeerConnection[] {
  const instances: FakePeerConnection[] = [];
  class Stubbed extends FakePeerConnection {
    constructor(config?: RTCConfiguration) {
      super(options, config);
      instances.push(this);
    }
  }
  vi.stubGlobal('RTCPeerConnection', Stubbed);
  return instances;
}
