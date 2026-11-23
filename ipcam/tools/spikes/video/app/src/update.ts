// SPIKE-ONLY (owner's request of 2026-11-23): the test app's own updates. The
// app asks GitHub which spike-video-v* releases exist; when one is newer than
// APP_VERSION, the start page offers it, and the shell (native-tauri.ts,
// native-android.ts) downloads the shell's asset, checks it against the SHA-256
// that GitHub lists for it, and installs it. GitHub is the trust anchor, as it
// is when the owner downloads the release by hand (T44: no signing keys yet).
// TODO (Phase 4): the product updates through the hub's updater (T27).

/** The repository's releases, newest first. Public: no token, and GitHub answers with CORS. */
export const RELEASES_URL = 'https://api.github.com/repos/AndreiBesliu/DevWorld/releases?per_page=30';

/** The tag of a release of this app, which is also APP_VERSION. */
const TAG = /^spike-video-v(\d+)\.(\d+)\.(\d+)$/;

/** How often the open app asks again. GitHub allows 60 unauthenticated requests an hour per address. */
export const RECHECK_MS = 6 * 60 * 60 * 1000;

/** What the start page offers and what the shell installs. */
export interface UpdateOffer {
  /** The release's tag, such as `spike-video-v0.1.2`. */
  version: string;
  /** The asset's download address on github.com. */
  url: string;
  /** The asset's SHA-256 as GitHub lists it, 64 lowercase hex digits. */
  sha256: string;
}

/** The download address that an asset of `tag` may have: the shells accept no other (the Windows shell's scope checks it again). */
export function assetUrlPattern(asset: string): RegExp {
  const name = asset.replace(/[.]/g, '\\.');
  return new RegExp(
    `^https://github\\.com/AndreiBesliu/DevWorld/releases/download/spike-video-v\\d+\\.\\d+\\.\\d+/${name}$`,
    'i',
  );
}

/** The three numbers of a release tag, or undefined for another tag. */
export function versionOf(tag: string): [number, number, number] | undefined {
  const m = TAG.exec(tag);
  return m ? [Number(m[1]), Number(m[2]), Number(m[3])] : undefined;
}

/** Whether release `a` is newer than release `b`; false when either is not a release tag. */
export function isNewer(a: string, b: string): boolean {
  const va = versionOf(a);
  const vb = versionOf(b);
  if (!va || !vb) return false;
  for (let i = 0; i < 3; i++) {
    const x = va[i] ?? 0;
    const y = vb[i] ?? 0;
    if (x !== y) return x > y;
  }
  return false;
}

interface ReleaseJson {
  tag_name?: unknown;
  draft?: unknown;
  assets?: unknown;
}

interface AssetJson {
  name?: unknown;
  browser_download_url?: unknown;
  digest?: unknown;
}

/**
 * The newest published release newer than `current` that has `asset` with a
 * SHA-256, from GitHub's answer; undefined when there is none. Drafts, other
 * tags and assets without a digest or with an unexpected address are skipped.
 */
export function pickUpdate(releases: unknown, current: string, asset: string): UpdateOffer | undefined {
  if (!Array.isArray(releases)) return undefined;
  const pattern = assetUrlPattern(asset);
  let best: UpdateOffer | undefined;
  for (const release of releases as (ReleaseJson | null)[]) {
    if (typeof release !== 'object' || release === null) continue;
    const tag = release.tag_name;
    if (typeof tag !== 'string' || release.draft === true || !isNewer(tag, current)) continue;
    if (best && !isNewer(tag, best.version)) continue;
    const found = Array.isArray(release.assets)
      ? (release.assets as AssetJson[]).find((a) => a.name === asset)
      : undefined;
    const url = found?.browser_download_url;
    const digest = found?.digest;
    if (typeof url !== 'string' || !pattern.test(url) || !url.includes(`/${tag}/`)) continue;
    if (typeof digest !== 'string') continue;
    const sha = /^sha256:([0-9a-fA-F]{64})$/.exec(digest)?.[1];
    if (!sha) continue;
    best = { version: tag, url, sha256: sha.toLowerCase() };
  }
  return best;
}

/** Asks GitHub for a newer release with `asset`; rejects when GitHub cannot be reached or answers with an error. */
export async function checkForUpdate(
  fetchFn: (url: string, init: RequestInit) => Promise<Response>,
  current: string,
  asset: string,
): Promise<UpdateOffer | undefined> {
  const response = await fetchFn(RELEASES_URL, { headers: { Accept: 'application/vnd.github+json' } });
  if (!response.ok) throw new Error(`GitHub answered ${String(response.status)}`);
  return pickUpdate(await response.json(), current, asset);
}
