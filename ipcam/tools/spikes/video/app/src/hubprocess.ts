// SPIKE-ONLY (owner's request of 2026-11-24, plan 2026-11-24-spike-hub-panel):
// spikehub started by the Windows shell (native.ts's ShellHub) as the start
// page sees it: the folder it runs in, which the page remembers, its state and
// the last lines it wrote, which the page shows instead of a console window.
// TODO (Phase 4): the product's hub is a Windows service (§4.9); the shell shows its state.
import { HubStartError, type RunningHub, type ShellHub } from './native';
import type { KeyValueStore } from './results';

/** Where the page remembers the hub's folder; a remembered folder starts the hub at launch. */
export const HUB_FOLDER_KEY = 'spike.hubFolder';
/** How many of the hub's lines the page keeps. */
export const LOG_LINES = 400;
/** What spikehub logs once it serves (its slog line `msg="spikehub listens"`). */
export const LISTENING = 'spikehub listens';
/** The hub's address on the PC that runs it (127.0.0.1: spikehub listens on IPv4 only, R38). */
export const LOCAL_HUB = 'http://127.0.0.1:7080';

/**
 * - `stopped`: not started, or stopped by the person.
 * - `ended`: it ended by itself, with its exit code (null when unknown).
 * - `missing`: the folder has no spikehub.exe.
 * - `failed`: nothing could start in the folder; `detail` is the system's reason.
 */
export type HubState =
  | { kind: 'stopped' }
  | { kind: 'starting' }
  | { kind: 'running'; listening: boolean }
  | { kind: 'stopping' }
  | { kind: 'ended'; code: number | null }
  | { kind: 'missing' }
  | { kind: 'failed'; detail: string };

/** The hub's last LOG_LINES lines, as it wrote them, without line breaks. */
export class HubLog {
  private readonly kept: string[] = [];
  private readonly max: number;

  constructor(max = LOG_LINES) {
    this.max = max;
  }

  /** Adds what the hub wrote; a text with line breaks counts as several lines. */
  push(text: string): void {
    for (const line of text.replace(/\r?\n$/, '').split(/\r?\n/)) this.kept.push(line);
    if (this.kept.length > this.max) this.kept.splice(0, this.kept.length - this.max);
  }

  lines(): readonly string[] {
    return this.kept;
  }
}

export interface HubControlDeps {
  shell: ShellHub;
  storage?: KeyValueStore | undefined;
  /** The state, the folder or the log changed. */
  onChange(): void;
  /** The hub logged LISTENING: it serves on LOCAL_HUB. */
  onListening(): void;
}

/** The hub that the page starts and stops through the shell. */
export class HubControl {
  readonly log = new HubLog();
  private current: HubState = { kind: 'stopped' };
  private chosen = '';
  private running: RunningHub | undefined;
  /** This start's hub logged LISTENING. */
  private listening = false;
  private readonly deps: HubControlDeps;

  constructor(deps: HubControlDeps) {
    this.deps = deps;
  }

  state(): HubState {
    return this.current;
  }

  /** The folder the hub starts in: the remembered one, else the shell's default, else empty. */
  folder(): string {
    return this.chosen;
  }

  /** Whether a folder was remembered, so that the hub may start by itself. */
  remembered(): boolean {
    return read(this.deps.storage, HUB_FOLDER_KEY) !== null;
  }

  /** Fills the folder: the remembered one, else the shell's default. */
  async load(): Promise<void> {
    const remembered = read(this.deps.storage, HUB_FOLDER_KEY);
    this.chosen = remembered ?? (await this.deps.shell.defaultFolder()) ?? '';
    this.deps.onChange();
  }

  /** Whether the hub runs or is on its way up or down, so that it cannot be started again. */
  busy(): boolean {
    const { kind } = this.current;
    return kind === 'starting' || kind === 'running' || kind === 'stopping';
  }

  /** Starts the hub in `folder` (trimmed, without a trailing backslash), which is remembered. */
  async start(folder: string): Promise<void> {
    if (this.busy()) return;
    const dir = folder.trim().replace(/[\\/]+$/, '');
    this.chosen = dir;
    if (dir === '') {
      this.set({ kind: 'failed', detail: '' });
      return;
    }
    write(this.deps.storage, HUB_FOLDER_KEY, dir);
    this.listening = false;
    this.set({ kind: 'starting' });
    try {
      const running = await this.deps.shell.start(dir, {
        line: (text) => {
          this.log.push(text);
          const heard = !this.listening && text.includes(LISTENING);
          if (heard) {
            this.listening = true;
            if (this.current.kind === 'running') this.current = { kind: 'running', listening: true };
          }
          this.deps.onChange();
          if (heard) this.deps.onListening();
        },
        exit: (end) => {
          this.running = undefined;
          if (this.current.kind === 'stopping') this.set({ kind: 'stopped' });
          else if (end === 'missing') this.set({ kind: 'missing' });
          else this.set({ kind: 'ended', code: end });
        },
      });
      // A hub that ended before spawn answered has already set its end.
      if (this.current.kind === 'starting') {
        this.running = running;
        this.set({ kind: 'running', listening: this.listening });
      }
    } catch (error) {
      this.set({ kind: 'failed', detail: error instanceof HubStartError ? error.detail : String(error) });
    }
  }

  /** Stops the hub; nothing when it does not run. */
  async stop(): Promise<void> {
    const running = this.running;
    if (!running || this.current.kind === 'stopping') return;
    this.set({ kind: 'stopping' });
    try {
      await running.stop();
    } finally {
      // exit() normally comes with the stop; without it, the hub is still taken as stopped.
      this.running = undefined;
      if (this.state().kind === 'stopping') this.set({ kind: 'stopped' });
    }
  }

  private set(state: HubState): void {
    this.current = state;
    this.deps.onChange();
  }
}

function read(storage: KeyValueStore | undefined, key: string): string | null {
  try {
    return storage?.getItem(key) ?? null;
  } catch {
    return null; // storage blocked: nothing remembered
  }
}

function write(storage: KeyValueStore | undefined, key: string, value: string): void {
  try {
    storage?.setItem(key, value);
  } catch {
    // Not remembered: the hub does not start by itself at the next launch.
  }
}
