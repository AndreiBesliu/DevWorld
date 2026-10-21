import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchCameras } from './cameras';
import { ConnectError } from './session';

const HUB = 'http://192.168.1.20:7080';

const LIST = [
  { id: 'sim-01', camera: 1, codec: 'H264', profiles: ['sub', 'main'] },
  { id: 'mx-high51', camera: 18, codec: 'H264', profileLevel: '640033', profiles: ['main', 'sub'] },
];

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('fetchCameras', () => {
  it('fetchCameras_readsTheHubList', async () => {
    const fetchMock = vi.fn<typeof fetch>(() => Promise.resolve(Response.json([...LIST, { id: 42 }])));
    vi.stubGlobal('fetch', fetchMock);

    const cameras = await fetchCameras(`${HUB}/`);

    expect(fetchMock.mock.calls[0]?.[0]).toBe(`${HUB}/spike/v1/cameras`);
    expect(cameras).toEqual(LIST); // an entry that is not a camera is left out
  });

  it('fetchCameras_hubDown_throwsConnectErrorWithTheUrl', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.reject(new TypeError('Failed to fetch'))));

    const failure = await fetchCameras(HUB).catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(ConnectError);
    expect(failure).toMatchObject({ kind: 'refused', url: HUB });
  });

  it('fetchCameras_notAList_isRejected', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>(() => Promise.resolve(new Response('<html>router login</html>'))));

    await expect(fetchCameras(HUB)).rejects.toMatchObject({ name: 'ConnectError', kind: 'rejected' });
  });
});
