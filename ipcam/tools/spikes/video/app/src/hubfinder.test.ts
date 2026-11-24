import { describe, expect, it } from 'vitest';
import { FIND_PARALLEL, hostsToAsk, hubUrlOf, networksOf, searchHubs, type FindState, type Search } from './hubfinder';
import type { LanAddress } from './native';

const home: LanAddress = { address: '192.168.1.57', prefix: 24 };

describe('the hosts the phone asks', () => {
  it('hostsToAsk_aSlash24_theOther253Hosts', () => {
    const hosts = hostsToAsk([home]);

    expect(hosts).toHaveLength(253);
    expect(hosts[0]).toBe('192.168.1.1');
    expect(hosts.at(-1)).toBe('192.168.1.254');
    expect(hosts).not.toContain('192.168.1.57');
    expect(hosts).not.toContain('192.168.1.0');
    expect(hosts).not.toContain('192.168.1.255');
  });

  it('hostsToAsk_aWiderSubnet_onlyTheSlash24OfThePhone', () => {
    // A /16 would be 65 534 hosts; the PC is almost always in the phone's /24.
    const hosts = hostsToAsk([{ address: '10.20.30.40', prefix: 16 }]);

    expect(hosts).toHaveLength(253);
    expect(hosts[0]).toBe('10.20.30.1');
    expect(networksOf([{ address: '10.20.30.40', prefix: 16 }])).toEqual(['10.20.30.0/24']);
  });

  it('hostsToAsk_aNarrowerSubnet_onlyItsHosts', () => {
    expect(hostsToAsk([{ address: '172.16.5.10', prefix: 29 }])).toEqual(['172.16.5.9', '172.16.5.11', '172.16.5.12', '172.16.5.13', '172.16.5.14']);
    expect(networksOf([{ address: '172.16.5.10', prefix: 29 }])).toEqual(['172.16.5.8/29']);
  });

  it('hostsToAsk_twoAddressesInOneSubnet_eachHostOnceAndNeitherPhoneAddress', () => {
    const hosts = hostsToAsk([home, { address: '192.168.1.58', prefix: 24 }]);

    expect(hosts).toHaveLength(252);
    expect(new Set(hosts).size).toBe(hosts.length);
    expect(networksOf([home, { address: '192.168.1.58', prefix: 24 }])).toEqual(['192.168.1.0/24']);
  });

  it('hostsToAsk_nothingToAsk_withoutAUsableAddress', () => {
    for (const address of [
      { address: '192.168.1.57', prefix: 31 },
      { address: '192.168.1.57', prefix: 32 },
      { address: 'fe80::1', prefix: 64 },
      { address: '192.168.1.300', prefix: 24 },
      { address: '192.168.1', prefix: 24 },
    ]) {
      expect(hostsToAsk([address]), address.address).toEqual([]);
      expect(networksOf([address]), address.address).toEqual([]);
    }
  });

  it('hubUrlOf_isWhatTheFieldTakes', () => {
    expect(hubUrlOf('192.168.1.23')).toBe('http://192.168.1.23:7080');
  });
});

/** A network in which `hubs` answer as spikehub after `ms`, and every other host fails. */
function network(hubs: string[], ms = 5) {
  const asked: string[] = [];
  let inFlight = 0;
  let mostInFlight = 0;
  const ask = async (url: string): Promise<void> => {
    asked.push(url);
    inFlight += 1;
    mostInFlight = Math.max(mostInFlight, inFlight);
    await new Promise((resolve) => setTimeout(resolve, ms));
    inFlight -= 1;
    if (!hubs.includes(url)) throw new Error('refused');
  };
  return { ask, asked, mostInFlight: () => mostInFlight };
}

describe('the search', () => {
  it('search_findsTheHubsAndShowsEachAsItAnswers', async () => {
    const net = network(['http://192.168.1.23:7080', 'http://192.168.1.200:7080']);
    const states: FindState[] = [];

    const found = await searchHubs({ lanAddresses: () => Promise.resolve([home]), ask: net.ask, show: (state) => states.push(state) }).done;

    expect(found).toEqual(['http://192.168.1.23:7080', 'http://192.168.1.200:7080']);
    expect(states[0]).toEqual({ kind: 'searching', networks: ['192.168.1.0/24'], found: [] });
    expect(states).toContainEqual({ kind: 'searching', networks: ['192.168.1.0/24'], found: ['http://192.168.1.23:7080'] });
    expect(states.at(-1)).toEqual({ kind: 'done', networks: ['192.168.1.0/24'], found });
    expect(net.asked).toHaveLength(253);
    expect(net.mostInFlight()).toBe(FIND_PARALLEL);
  });

  it('search_noHub_endsWithNone', async () => {
    const states: FindState[] = [];

    const found = await searchHubs({ lanAddresses: () => Promise.resolve([home]), ask: network([]).ask, show: (state) => states.push(state) }).done;

    expect(found).toEqual([]);
    expect(states.at(-1)).toEqual({ kind: 'done', networks: ['192.168.1.0/24'], found: [] });
  });

  it('search_noWifi_saysSoAndAsksNobody', async () => {
    const net = network([]);
    const states: FindState[] = [];

    await searchHubs({ lanAddresses: () => Promise.resolve([]), ask: net.ask, show: (state) => states.push(state) }).done;

    expect(states).toEqual([{ kind: 'no-network' }]);
    expect(net.asked).toEqual([]);
  });

  it('search_theShellFails_saysWhat', async () => {
    const states: FindState[] = [];

    await searchHubs({ lanAddresses: () => Promise.reject(new Error('not implemented')), ask: network([]).ask, show: (state) => states.push(state) })
      .done;

    expect(states).toEqual([{ kind: 'failed', detail: 'not implemented' }]);
  });

  it('search_stopped_asksNoMoreHostsAndEnds', async () => {
    // The person picks a hub, or starts a run, while the search goes on: it is stopped at the 6th host.
    const asked: string[] = [];
    const states: FindState[] = [];
    const started: { search?: Search } = {};
    const ask = async (url: string): Promise<void> => {
      asked.push(url);
      if (asked.length === 6) started.search?.stop();
      await new Promise((resolve) => setTimeout(resolve, 1));
      if (url !== 'http://192.168.1.2:7080') throw new Error('refused');
    };
    const search = searchHubs({ lanAddresses: () => Promise.resolve([home]), ask, show: (state) => states.push(state), parallel: 4 });
    started.search = search;

    const found = await search.done;

    expect(asked).toHaveLength(6);
    expect(found).toEqual(['http://192.168.1.2:7080']);
    expect(states.at(-1)).toEqual({ kind: 'done', networks: ['192.168.1.0/24'], found });
  });
});
