import { describe, expect, it, vi } from 'vitest';
import { HUB_FOLDER_KEY, HubControl, HubLog, LISTENING } from './hubprocess';
import { HubStartError, type HubOutput, type ShellHub } from './native';

class MemoryStore {
  readonly map = new Map<string, string>();
  getItem(key: string): string | null {
    return this.map.get(key) ?? null;
  }
  setItem(key: string, value: string): void {
    this.map.set(key, value);
  }
}

/** A shell double: the folders it started in, the output of the last start, and its stops. */
function fakeShell(opts: { defaultFolder?: string; fail?: string } = {}): {
  shell: ShellHub;
  out: () => HubOutput;
  folders: string[];
  stops: () => number;
} {
  let out: HubOutput | undefined;
  const folders: string[] = [];
  let stops = 0;
  const shell: ShellHub = {
    start: (folder, o) => {
      folders.push(folder);
      if (opts.fail !== undefined) return Promise.reject(new HubStartError(opts.fail));
      out = o;
      return Promise.resolve({
        stop: () => {
          stops++;
          o.exit(null);
          return Promise.resolve();
        },
      });
    },
    defaultFolder: () => Promise.resolve(opts.defaultFolder),
  };
  return {
    shell,
    out: () => {
      if (!out) throw new Error('nothing was started');
      return out;
    },
    folders,
    stops: () => stops,
  };
}

function control(shell: ShellHub, storage = new MemoryStore()) {
  const onChange = vi.fn();
  const onListening = vi.fn();
  return { hub: new HubControl({ shell, storage, onChange, onListening }), onChange, onListening, storage };
}

describe('HubLog', () => {
  it('hubLog_splitsLinesAndKeepsTheLast', () => {
    const log = new HubLog(3);
    log.push('one\r\n');
    log.push('two\nthree\n');
    log.push('four');
    expect(log.lines()).toEqual(['two', 'three', 'four']);
  });
});

describe('HubControl', () => {
  it('hubControl_offersTheShellsDefault_untilAFolderIsRemembered', async () => {
    const fake = fakeShell({ defaultFolder: 'C:\\Users\\a\\Downloads\\spikehub-windows' });
    const { hub, storage } = control(fake.shell);
    await hub.load();
    expect(hub.folder()).toBe('C:\\Users\\a\\Downloads\\spikehub-windows');
    expect(hub.remembered()).toBe(false);

    await hub.start('  D:\\teste\\spikehub-windows\\ ');
    expect(fake.folders).toEqual(['D:\\teste\\spikehub-windows']);
    expect(storage.getItem(HUB_FOLDER_KEY)).toBe('D:\\teste\\spikehub-windows');
    const next = control(fakeShell({ defaultFolder: 'elsewhere' }).shell, storage).hub;
    await next.load();
    expect(next.folder()).toBe('D:\\teste\\spikehub-windows');
    expect(next.remembered()).toBe(true);
  });

  it('hubControl_runsLogsAndHearsListening', async () => {
    const fake = fakeShell();
    const { hub, onListening } = control(fake.shell);
    await hub.start('C:\\hub');
    expect(hub.state()).toEqual({ kind: 'running', listening: false });
    expect(hub.busy()).toBe(true);

    fake.out().line('Adresele acestui calculator în rețeaua locală:');
    fake.out().line(`time=2026-11-24T12:04:43 level=INFO msg="${LISTENING}" port=7080`);
    fake.out().line(`a second "${LISTENING}" changes nothing`);
    expect(hub.state()).toEqual({ kind: 'running', listening: true });
    expect(onListening).toHaveBeenCalledTimes(1);
    expect(hub.log.lines()).toHaveLength(3);

    // A second start while it runs does nothing.
    await hub.start('C:\\other');
    expect(fake.folders).toEqual(['C:\\hub']);
  });

  it('hubControl_endsByItself_withItsCode', async () => {
    const fake = fakeShell();
    const { hub } = control(fake.shell);
    await hub.start('C:\\hub');
    fake.out().line('spikehub: nu pot citi fișierele media');
    fake.out().exit(1);
    expect(hub.state()).toEqual({ kind: 'ended', code: 1 });
    expect(hub.busy()).toBe(false);
  });

  it('hubControl_missingExe', async () => {
    const fake = fakeShell();
    const { hub } = control(fake.shell);
    await hub.start('C:\\empty');
    fake.out().exit('missing');
    expect(hub.state()).toEqual({ kind: 'missing' });
  });

  it('hubControl_stopIsStopped_notEnded', async () => {
    const fake = fakeShell();
    const { hub } = control(fake.shell);
    await hub.start('C:\\hub');
    await hub.stop();
    await hub.stop();
    expect(fake.stops()).toBe(1);
    expect(hub.state()).toEqual({ kind: 'stopped' });
    // It can start again.
    await hub.start('C:\\hub');
    expect(hub.state().kind).toBe('running');
  });

  it('hubControl_startFailure_andEmptyFolder', async () => {
    const { hub } = control(fakeShell({ fail: 'The directory name is invalid. (os error 267)' }).shell);
    await hub.start('C:\\nowhere');
    expect(hub.state()).toEqual({ kind: 'failed', detail: 'The directory name is invalid. (os error 267)' });

    const empty = fakeShell();
    const second = control(empty.shell);
    await second.hub.start('   ');
    expect(second.hub.state()).toEqual({ kind: 'failed', detail: '' });
    expect(empty.folders).toEqual([]);
    expect(second.storage.getItem(HUB_FOLDER_KEY)).toBeNull();
  });

  it('hubControl_aHubThatEndsAtOnce_staysEnded', async () => {
    // cmd can end (9009) before spawn's answer reaches the page.
    const shell: ShellHub = {
      start: (_folder, out) => {
        out.exit('missing');
        return Promise.resolve({ stop: () => Promise.resolve() });
      },
      defaultFolder: () => Promise.resolve(undefined),
    };
    const { hub } = control(shell);
    await hub.start('C:\\empty');
    expect(hub.state()).toEqual({ kind: 'missing' });
  });
});
