// The hub's camera list (GET /spike/v1/cameras, cmd/spikehub/catalog.go).
import { requestHub } from './hubhttp';

export interface HubCamera {
  /** `sim-01` to `sim-16`, `mx-h265`, `mx-high51`, `real-1` and so on. */
  id: string;
  /** The number that the hub's `sent` messages carry for this camera (R1, R3). */
  camera: number;
  codec: string;
  /** The profile-level-id of a main stream that a browser may not decode (mx-high51). */
  profileLevel?: string;
  profiles: string[];
}

const CAMERAS_TIMEOUT_MS = 10_000;

function isCamera(value: unknown): value is HubCamera {
  if (typeof value !== 'object' || value === null) return false;
  const c = value as Record<string, unknown>;
  return (
    typeof c.id === 'string' &&
    typeof c.camera === 'number' &&
    typeof c.codec === 'string' &&
    Array.isArray(c.profiles) &&
    c.profiles.every((profile) => typeof profile === 'string') &&
    (c.profileLevel === undefined || typeof c.profileLevel === 'string')
  );
}

/** The hub's cameras. Rejects with a ConnectError, like the offer, when the hub cannot be reached. */
export function fetchCameras(hubUrl: string): Promise<HubCamera[]> {
  const url = hubUrl.replace(/\/+$/, '');
  return requestHub(
    url,
    '/spike/v1/cameras',
    { cache: 'no-store' },
    async (response) => {
      const list: unknown = await response.json();
      if (!Array.isArray(list)) throw new Error('the camera list is not a list');
      return list.filter(isCamera);
    },
    CAMERAS_TIMEOUT_MS,
  );
}

/** The simulated cameras, sim-01 first. */
export function simCameras(cameras: HubCamera[]): HubCamera[] {
  return cameras.filter((camera) => camera.id.startsWith('sim-'));
}
