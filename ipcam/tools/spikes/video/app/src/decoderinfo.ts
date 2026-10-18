// SPIKE-ONLY (phase0b video spike plan, Global Constraints): Chrome reports the
// decoder of a received track (decoderImplementation, powerEfficientDecoder in
// getStats) only while the page holds a camera capture, so S3 holds a 1-pixel
// one while it samples MV4. Production reads mediaCapabilities instead (§6.5).
// The capture is never shown, recorded or sent.

/** The part of navigator.mediaDevices that the capture uses. */
export interface CaptureDevices {
  getUserMedia(constraints: MediaStreamConstraints): Promise<MediaStream>;
}

/** How long a permission prompt may stay unanswered. */
const PROMPT_TIMEOUT_MS = 20_000;

const stopAll = (stream: MediaStream): void => {
  for (const track of stream.getTracks()) track.stop();
};

/**
 * Holds a 1-pixel video capture and returns the function that releases it, or
 * `n/a` when there is no camera, the permission is refused or the prompt stays
 * unanswered for 20 s (a capture granted after that is stopped at once).
 */
export async function captureForDecoderInfo(
  devices: CaptureDevices | undefined = globalThis.navigator?.mediaDevices,
): Promise<(() => void) | 'n/a'> {
  if (!devices) return 'n/a';
  const capture = devices.getUserMedia({
    audio: false,
    video: { width: { ideal: 1 }, height: { ideal: 1 }, frameRate: { ideal: 1 } },
  });
  let timer: ReturnType<typeof setTimeout> | undefined;
  const timeout = new Promise<'n/a'>((resolve) => {
    timer = setTimeout(() => {
      resolve('n/a');
    }, PROMPT_TIMEOUT_MS);
  });
  try {
    const outcome = await Promise.race([capture, timeout]);
    if (outcome === 'n/a') {
      capture.then(stopAll, () => undefined);
      return 'n/a';
    }
    return () => {
      stopAll(outcome);
    };
  } catch {
    return 'n/a';
  } finally {
    clearTimeout(timer);
  }
}
