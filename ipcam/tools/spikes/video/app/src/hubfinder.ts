// SPIKE-ONLY (owner's request of 2026-11-24, plan 2026-11-24-spike-skip-wait-find-hub):
// the phone looks for spikehub itself, so that the owner picks the hub from a
// list instead of typing the PC's address and guessing which of its adapters
// (Ethernet, VirtualBox, WSL) the phone can reach. The Android shell tells the
// phone's Wi-Fi addresses (native.ts, lanAddresses); every other host of their
// subnets is asked for spikehub's progress on port 7080, and the hosts that
// answer as spikehub are the hubs. The product finds its hub by mDNS instead
// (spec §6.6), so none of this carries over. The addresses are for the screen
// only: none goes into a result or a log (PH0-22).
import type { LanAddress } from './native';

/** spikehub's port. */
export const HUB_PORT = 7080;
/** How long one host may take to answer: a hub on the LAN answers in milliseconds, an absent host never. */
export const FIND_TIMEOUT_MS = 1500;
/** How many hosts are asked at once: a /24 takes 254 / 32 × 1.5 s, about 12 s, at most. */
export const FIND_PARALLEL = 32;
/** The widest subnet searched whole; in a wider one, only the /24 that holds the phone's address. */
const WIDEST_PREFIX = 24;

/** The address as a 32-bit number, or undefined when it is not a dotted IPv4 address. */
function toNumber(ip: string): number | undefined {
  const parts = ip.split('.');
  if (parts.length !== 4) return undefined;
  let value = 0;
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part) || Number(part) > 255) return undefined;
    value = value * 256 + Number(part);
  }
  return value;
}

const toText = (n: number): string => [Math.floor(n / 2 ** 24) % 256, Math.floor(n / 2 ** 16) % 256, Math.floor(n / 256) % 256, n % 256].join('.');

/** The subnet searched for an address: its first address and its size, or undefined when it has no other host. */
function subnetOf({ address, prefix }: LanAddress): { network: number; size: number; prefix: number } | undefined {
  const ip = toNumber(address);
  if (ip === undefined || !Number.isInteger(prefix) || prefix < 0 || prefix > 30) return undefined;
  const searched = Math.max(prefix, WIDEST_PREFIX);
  const size = 2 ** (32 - searched);
  return { network: ip - (ip % size), size, prefix: searched };
}

/** The subnets that the search covers, as the screen shows them, such as `192.168.1.0/24`. */
export function networksOf(addresses: readonly LanAddress[]): string[] {
  const out: string[] = [];
  for (const address of addresses) {
    const subnet = subnetOf(address);
    if (subnet === undefined) continue;
    const label = `${toText(subnet.network)}/${subnet.prefix}`;
    if (!out.includes(label)) out.push(label);
  }
  return out;
}

/**
 * The hosts to ask, in order: for each address, the hosts of its subnet (of
 * the /24 that holds it, when the subnet is wider), without the network and
 * broadcast addresses, the phone's own addresses and repeats.
 */
export function hostsToAsk(addresses: readonly LanAddress[]): string[] {
  const own = new Set(addresses.map((address) => address.address));
  const hosts = new Set<string>();
  for (const address of addresses) {
    const subnet = subnetOf(address);
    if (subnet === undefined) continue;
    for (let host = subnet.network + 1; host < subnet.network + subnet.size - 1; host += 1) {
      const text = toText(host);
      if (!own.has(text)) hosts.add(text);
    }
  }
  return [...hosts];
}

/** The hub address of a host, as the field takes it. */
export const hubUrlOf = (host: string): string => `http://${host}:${HUB_PORT}`;

/** Where the search is, for the page. `found` holds the hub addresses in the order the hosts were asked. */
export type FindState =
  | { kind: 'searching'; networks: string[]; found: string[] }
  | { kind: 'done'; networks: string[]; found: string[] }
  /** The phone is on no Wi-Fi or Ethernet network with a private IPv4 address. */
  | { kind: 'no-network' }
  /** The shell could not tell the addresses; `detail` is what it said. */
  | { kind: 'failed'; detail: string };

export interface SearchDeps {
  /** The phone's addresses (the Android shell). */
  lanAddresses(): Promise<LanAddress[]>;
  /** Asks a hub address; resolves when spikehub answered there, rejects otherwise (nothing, or not spikehub). */
  ask(url: string): Promise<void>;
  /** The state at the start, at each hub found and at the end. */
  show(state: FindState): void;
  /** How many hosts at once; FIND_PARALLEL unless a test says otherwise. */
  parallel?: number;
}

/** A search in progress: `done` resolves with the hub addresses found; `stop` asks no more hosts. */
export interface Search {
  done: Promise<string[]>;
  stop(): void;
}

/**
 * Looks for spikehub in the phone's subnets. A stopped search asks no more
 * hosts; the ones it asked already may still answer, and it ends once they have.
 */
export function searchHubs(deps: SearchDeps): Search {
  let stopped = false;
  const done = (async (): Promise<string[]> => {
    let addresses: LanAddress[];
    try {
      addresses = await deps.lanAddresses();
    } catch (error) {
      deps.show({ kind: 'failed', detail: error instanceof Error ? error.message : String(error) });
      return [];
    }
    const networks = networksOf(addresses);
    const hosts = hostsToAsk(addresses);
    if (hosts.length === 0) {
      deps.show({ kind: 'no-network' });
      return [];
    }
    const answered: (string | undefined)[] = [];
    const found = (): string[] => answered.filter((url): url is string => url !== undefined);
    deps.show({ kind: 'searching', networks, found: [] });
    let next = 0;
    const worker = async (): Promise<void> => {
      while (!stopped && next < hosts.length) {
        const index = next;
        next += 1;
        const url = hubUrlOf(hosts[index] ?? '');
        try {
          await deps.ask(url);
        } catch {
          continue; // nobody there, or not spikehub
        }
        answered[index] = url;
        if (!stopped) deps.show({ kind: 'searching', networks, found: found() });
      }
    };
    await Promise.all(Array.from({ length: Math.min(deps.parallel ?? FIND_PARALLEL, hosts.length) }, worker));
    deps.show({ kind: 'done', networks, found: found() });
    return found();
  })();
  return {
    done,
    stop: () => {
      stopped = true;
    },
  };
}
