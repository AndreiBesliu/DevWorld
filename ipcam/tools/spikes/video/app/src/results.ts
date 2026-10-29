// The results of the scenarios and their way to the hub. A result is written to
// localStorage before it is uploaded and stays there until the hub has it, so a
// closed hub or a dropped Wi-Fi never loses a run (Review Focus 5).
import type { Platform } from './device';

export type ScenarioId = 'S1' | 'S2' | 'S3-4' | 'S3-9' | 'S3-16' | 'S4' | 'S5' | 'S6' | 'S7' | 'S8' | 'S9' | 'S10' | 'AGE';

export const SCENARIO_IDS: readonly ScenarioId[] = [
  'S1',
  'S2',
  'S3-4',
  'S3-9',
  'S3-16',
  'S4',
  'S5',
  'S6',
  'S7',
  'S8',
  'S9',
  'S10',
  // The recorder spike's MI4 bench (phase0c1 plan, Task 10), posted to reccontroller.
  'AGE',
];

/**
 * Why a run ended as interrupted (left out of the verdicts, to be repeated):
 * - `hidden`: the page was hidden `atS` s into the run (the screen turned off, the app went to the background);
 * - `session-lost`: the hub session was lost `atS` s into the run, `how` says how (R49);
 * - `charging`: the phone was on the charger at a reading `atS` s into an S3 run (R48);
 * - `early-return`: S5's background lasted `backgroundS` s, less than the 2 min MV9 needs;
 * - `pip-left`: S6's picture-in-picture lasted `pipS` s, less than the 5 min MV8 needs.
 *
 * The run's notes carry the same as an `interrupted: …` line.
 */
export type InterruptReason =
  | { kind: 'hidden'; atS: number }
  | { kind: 'session-lost'; atS: number; how: string }
  | { kind: 'charging'; atS: number }
  | { kind: 'early-return'; backgroundS: number }
  | { kind: 'pip-left'; pipS: number };

/**
 * One run of a scenario, as the hub stores it and the report reads it. The
 * sample keys and the notes of each scenario are documented in its file under
 * scenarios/. Nothing in it names an address, a network or a camera URL (PH0-22).
 */
export interface ScenarioResult {
  scenario: ScenarioId;
  /**
   * The device as the page and its shell tell it: `ramMb` is the physical
   * memory in MiB and `gpu` the display adapters, both from the shell when it
   * reports them (native-5).
   */
  device: { platform: Platform; model?: string; os?: string; webview?: string; ramMb?: number; gpu?: string; ua: string };
  /** ISO 8601, from the device's clock: it dates the run and measures nothing. */
  startedAt: string;
  durationS: number;
  /** The run ended before it measured what it should (see `interruption`): left out of the verdicts. */
  interrupted: boolean;
  /** Why, when `interrupted`. Absent in results written before this field existed: read them as `hidden`. */
  interruption?: InterruptReason;
  /** Every `jitter` report of the hub during the run: the p99 send deviation over the 10 s before it (R28). */
  hubJitterMs: number[];
  samples: Record<string, number[]>;
  notes: string[];
}

/** The part of localStorage that the app uses. */
export interface KeyValueStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

/** The localStorage key of the results that the hub does not have yet. */
export const PENDING_KEY = 'spike.pending';

const UPLOAD_TIMEOUT_MS = 10_000;

const URL_PATTERN = /\b[a-z][a-z0-9+.-]*:\/\/\S+/gi;
const IPV4_PATTERN = /\b\d{1,3}(?:\.\d{1,3}){3}(?::\d{1,5})?\b/g;
/** An IPv6 literal: with `::`, or eight groups in full; with a zone and in brackets with a port. */
const IPV6_PATTERN =
  /\[?(?:(?:[0-9a-f]{1,4}:)*[0-9a-f]{0,4}::(?:[0-9a-f]{0,4}(?::[0-9a-f]{1,4})*)|(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4})(?:%[\w.]+)?(?:\](?::\d{1,5})?)?/gi;

/**
 * The text without URLs and IP literals (PH0-22): error messages carry the
 * hub's address, which is a LAN address of the owner's home, and must not reach
 * a result. The notes of every result pass through it.
 */
export function scrubText(text: string): string {
  return text.replace(URL_PATTERN, '<url>').replace(IPV6_PATTERN, '<ip>').replace(IPV4_PATTERN, '<ip>');
}

/** The results waiting for the hub, in localStorage when it works and in memory when it does not. */
export class ResultsQueue {
  private readonly storage: KeyValueStore | undefined;
  private readonly fetch: typeof fetch;
  private readonly hubUrl: () => string;
  /** The list when localStorage refuses it (a private window, blocked site data). */
  private memory: ScenarioResult[] | undefined;
  private flushing: Promise<number> | undefined;

  constructor(opts: { storage?: KeyValueStore | undefined; fetch?: typeof fetch; hubUrl: () => string }) {
    this.storage = opts.storage;
    this.fetch = opts.fetch ?? ((input, init) => fetch(input, init));
    this.hubUrl = opts.hubUrl;
  }

  /** Stores the result; flush() uploads it. */
  add(result: ScenarioResult): void {
    this.write([...this.read(), result]);
  }

  pending(): number {
    return this.read().length;
  }

  /**
   * Uploads every pending result, one after the other, and keeps the ones the
   * hub did not take. Resolves with the number still pending. A call during an
   * upload gets that upload, which also sends the results added meanwhile
   * (each result is tried once per flush).
   */
  flush(): Promise<number> {
    this.flushing ??= this.upload().finally(() => {
      this.flushing = undefined;
    });
    return this.flushing;
  }

  private async upload(): Promise<number> {
    const failed: ScenarioResult[] = [];
    // Results added during the upload come after the ones read before, so a
    // second pass takes those that appeared during the first, and so on.
    for (let tried = 0, pending = this.read(); tried < pending.length; pending = this.read()) {
      for (const result of pending.slice(tried)) {
        if (!(await this.post(result))) failed.push(result);
      }
      tried = pending.length;
    }
    this.write(failed);
    return failed.length;
  }

  private async post(result: ScenarioResult): Promise<boolean> {
    const controller = new AbortController();
    const timer = setTimeout(() => {
      controller.abort();
    }, UPLOAD_TIMEOUT_MS);
    try {
      // SPIKE-ONLY (phase0b video spike plan, Global Constraints): plain HTTP to the hub on the LAN, port 7080.
      // text/plain needs no CORS preflight from the shells' origins; the hub reads the body as JSON.
      const response = await this.fetch(`${this.hubUrl().replace(/\/+$/, '')}/spike/v1/results`, {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: JSON.stringify(result),
        signal: controller.signal,
      });
      return response.ok;
    } catch {
      return false; // the hub is closed or out of reach: the next flush tries again
    } finally {
      clearTimeout(timer);
    }
  }

  private read(): ScenarioResult[] {
    if (this.memory) return [...this.memory];
    try {
      const parsed: unknown = JSON.parse(this.storage?.getItem(PENDING_KEY) ?? '[]');
      return Array.isArray(parsed) ? (parsed as ScenarioResult[]) : [];
    } catch {
      return [];
    }
  }

  private write(results: ScenarioResult[]): void {
    if (!this.memory) {
      try {
        if (this.storage) {
          this.storage.setItem(PENDING_KEY, JSON.stringify(results));
          return;
        }
      } catch {
        // Fall through to memory: the run survives until the page closes.
      }
    }
    this.memory = [...results];
  }
}

/** The test hook of autorun (R5): every finished result, in order. */
export interface ResultsTarget {
  __spikeResults?: ScenarioResult[];
}

/** Shows a finished result to the test hook, stores it and uploads what is pending. */
export async function publish(
  result: ScenarioResult,
  queue: ResultsQueue,
  target: ResultsTarget = globalThis as ResultsTarget,
): Promise<number> {
  target.__spikeResults ??= [];
  target.__spikeResults.push(result);
  queue.add(result);
  return queue.flush();
}
