import { describe, expect, it, vi } from 'vitest';
import { assetUrlPattern, checkForUpdate, isNewer, pickUpdate, RELEASES_URL, versionOf } from './update';

const SHA = 'a8ef1d42f1add10206b84d77d2f8c1b1f41ba3b0870ab3223e8df6ba41374c50';
const base = 'https://github.com/AndreiBesliu/DevWorld/releases/download';

/** A release as GitHub's API lists it, with the spike APK and the Windows installer. */
function release(tag: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    tag_name: tag,
    draft: false,
    prerelease: true,
    assets: ['ipcam-spike.apk', 'ipcam-desktop-setup.exe', 'SHA256SUMS'].map((name) => ({
      name,
      browser_download_url: `${base}/${tag}/${name}`,
      digest: `sha256:${SHA}`,
    })),
    ...extra,
  };
}

describe('versions', () => {
  it('update_versionOf_readsReleaseTagsOnly', () => {
    expect(versionOf('spike-video-v0.1.12')).toEqual([0, 1, 12]);
    expect(versionOf('spike-recording-v0.1.0')).toBeUndefined();
    expect(versionOf('spike-video-v0.1')).toBeUndefined();
  });

  it('update_isNewer_comparesNumbersNotText', () => {
    expect(isNewer('spike-video-v0.1.10', 'spike-video-v0.1.9')).toBe(true);
    expect(isNewer('spike-video-v0.2.0', 'spike-video-v0.1.9')).toBe(true);
    expect(isNewer('spike-video-v0.1.2', 'spike-video-v0.1.2')).toBe(false);
    expect(isNewer('spike-video-v0.1.1', 'spike-video-v0.1.2')).toBe(false);
    expect(isNewer('other', 'spike-video-v0.1.2')).toBe(false);
  });
});

describe('pickUpdate', () => {
  it('update_pick_offersTheNewestReleaseWithTheAsset', () => {
    const releases = [
      release('spike-video-v0.1.3'),
      release('spike-recording-v9.0.0'),
      release('spike-video-v0.1.2'),
      release('spike-video-v0.1.1'),
    ];
    expect(pickUpdate(releases, 'spike-video-v0.1.1', 'ipcam-spike.apk')).toEqual({
      version: 'spike-video-v0.1.3',
      url: `${base}/spike-video-v0.1.3/ipcam-spike.apk`,
      sha256: SHA,
    });
  });

  it('update_pick_nothingWhenCurrentIsNewest', () => {
    const releases = [release('spike-video-v0.1.2'), release('spike-video-v0.1.1')];
    expect(pickUpdate(releases, 'spike-video-v0.1.2', 'ipcam-desktop-setup.exe')).toBeUndefined();
  });

  it('update_pick_skipsDraftsAndReleasesWithoutAUsableAsset', () => {
    const noDigest = release('spike-video-v0.1.5');
    (noDigest.assets as Record<string, unknown>[]).forEach((a) => {
      delete a.digest;
    });
    const elsewhere = release('spike-video-v0.1.4');
    (elsewhere.assets as Record<string, unknown>[]).forEach((a) => {
      a.browser_download_url = `https://example.com/spike-video-v0.1.4/${String(a.name)}`;
    });
    const otherTagInUrl = release('spike-video-v0.1.7');
    (otherTagInUrl.assets as Record<string, unknown>[]).forEach((a) => {
      a.browser_download_url = `${base}/spike-video-v0.1.0/${String(a.name)}`;
    });
    const releases = [
      release('spike-video-v0.1.9', { draft: true }),
      otherTagInUrl,
      noDigest,
      elsewhere,
      release('spike-video-v0.1.3', { assets: [] }),
      release('spike-video-v0.1.2'),
    ];
    expect(pickUpdate(releases, 'spike-video-v0.1.1', 'ipcam-spike.apk')?.version).toBe('spike-video-v0.1.2');
  });

  it('update_pick_toleratesAnythingGitHubMightSend', () => {
    expect(pickUpdate({ message: 'API rate limit exceeded' }, 'spike-video-v0.1.1', 'ipcam-spike.apk')).toBeUndefined();
    expect(pickUpdate([null, 3, { tag_name: 5 }], 'spike-video-v0.1.1', 'ipcam-spike.apk')).toBeUndefined();
  });

  it('update_assetUrlPattern_acceptsOnlyThisRepositorysReleases', () => {
    const p = assetUrlPattern('ipcam-desktop-setup.exe');
    expect(p.test(`${base}/spike-video-v0.1.2/ipcam-desktop-setup.exe`)).toBe(true);
    expect(p.test('https://github.com/andreibesliu/devworld/releases/download/spike-video-v0.1.2/ipcam-desktop-setup.exe')).toBe(true);
    expect(p.test(`${base}/spike-video-v0.1.2/ipcam-desktop-setupXexe`)).toBe(false);
    expect(p.test('https://github.com/someone/DevWorld/releases/download/spike-video-v0.1.2/ipcam-desktop-setup.exe')).toBe(false);
    expect(p.test(`${base}/spike-video-v0.1.2/ipcam-desktop-setup.exe?x`)).toBe(false);
  });
});

describe('checkForUpdate', () => {
  it('update_check_asksGitHubAndPicks', async () => {
    const fetchFn = vi.fn<(url: string, init: RequestInit) => Promise<Response>>(() =>
      Promise.resolve(new Response(JSON.stringify([release('spike-video-v0.1.2')]))),
    );
    await expect(checkForUpdate(fetchFn, 'spike-video-v0.1.1', 'ipcam-spike.apk')).resolves.toMatchObject({
      version: 'spike-video-v0.1.2',
    });
    expect(fetchFn.mock.calls[0]?.[0]).toBe(RELEASES_URL);
  });

  it('update_check_rejectsOnAnErrorAnswer', async () => {
    const fetchFn = vi.fn(() => Promise.resolve(new Response('{}', { status: 403 })));
    await expect(checkForUpdate(fetchFn, 'spike-video-v0.1.1', 'ipcam-spike.apk')).rejects.toThrow('403');
  });
});
