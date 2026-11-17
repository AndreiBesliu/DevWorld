// The Firefox preflight of the automatic run (post-release task A): before
// the run starts, Firefox must decode H.264, which it does only with Cisco's
// OpenH264 plugin. A short real decode decides: the substream of sim-01 on one
// tile until its first frame, within 10 s. Without the plugin Firefox offers
// no H.264 and the hub refuses the offer (`rejected`), which is a "no" too. The
// page then shows the guide's steps for the plugin and does not start.
//
// MediaCapabilities.decodingInfo is not asked: its `webrtc` type is not
// supported alike in every browser, and a wrong "no" would block a Firefox that
// decodes. The real decode is the proof.
import { simCameras } from './cameras';
import { ConnectError } from './hubhttp';
import type { ScenarioEnv } from './scenarios/context';
import { liveSource, onPresentedFrames } from './scenarios/frames';
import type { SpikeSession } from './session';

/** How long the check waits for the bind and the first frame together. */
export const H264_CHECK_TIMEOUT_MS = 10_000;

/**
 * Whether the page decodes the band stream's H.264. Rejects only when the hub
 * cannot be reached (a ConnectError other than `rejected`), which the page
 * explains as for any run.
 */
export async function decodesH264(env: ScenarioEnv, timeoutMs = H264_CHECK_TIMEOUT_MS): Promise<boolean> {
  const [cam] = simCameras(env.cameras);
  if (!cam) return false;
  let session: SpikeSession;
  try {
    session = await env.openSession(env.hubUrl, 1);
  } catch (error) {
    if (error instanceof ConnectError && error.kind === 'rejected') return false;
    throw error;
  }
  env.stage.open(env.t('h264Checking'));
  const player = env.createPlayer({ slot: 0 });
  const cleanups: (() => void)[] = [];
  try {
    const [video] = env.stage.grid(1);
    if (!video) return false;
    video.muted = true;
    video.playsInline = true;
    player.attach(video, {
      webrtcTrack: (slot) => Promise.resolve(session.track(slot)),
      byteStream: () => Promise.reject(new Error('the check plays live video only')),
    });
    const outcome = new Promise<boolean>((resolve) => {
      cleanups.push(onPresentedFrames(video, () => resolve(true)));
      cleanups.push(player.on('error', () => resolve(false)));
      cleanups.push(env.clock.setTimer(() => resolve(false), timeoutMs));
    });
    const bound = session.bind(0, cam.id, 'sub').then(
      () => true,
      () => false,
    );
    await player.play(liveSource(cam.id, 'sub'));
    if (!(await Promise.race([bound, outcome]))) return false;
    return await outcome;
  } catch {
    return false;
  } finally {
    for (const cleanup of cleanups) cleanup();
    player.stop();
    session.close();
    env.stage.reset();
  }
}
