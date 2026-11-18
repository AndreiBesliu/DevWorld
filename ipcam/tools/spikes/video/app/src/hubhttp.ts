// The spike app's HTTP calls to the hub that a scenario cannot start without:
// the offer (session.ts) and the camera list (cameras.ts). A failure is
// classified the same way for both, so that the start page can say why the
// phone cannot reach the hub (Review Focus 1).

/**
 * - `timeout`: no answer in time, the typical sign of AP isolation or a firewall that drops the traffic.
 * - `refused`: the connection failed at once, the typical sign of a wrong address or a hub that is not running.
 * - `rejected`: the hub answered with an HTTP error.
 */
export type ConnectFailure = 'timeout' | 'refused' | 'rejected';

/**
 * The app could not reach the hub. `url` is the hub address that was tried.
 * The message embeds that address, so it is for the screen only: a result must
 * never carry it (PH0-22); `kind` is what a result may record.
 */
export class ConnectError extends Error {
  readonly kind: ConnectFailure;
  readonly url: string;
  /** What went wrong as the browser or the hub put it (for a `rejected`, the hub's answer); for the screen too. */
  readonly detail: string;

  constructor(kind: ConnectFailure, url: string, detail: string) {
    super(`${kind}: ${detail} (${url})`);
    this.name = 'ConnectError';
    this.kind = kind;
    this.url = url;
    this.detail = detail;
  }
}

// SPIKE-ONLY (phase0b video spike plan, Global Constraints): the app talks to
// the hub over plain HTTP on the LAN, port 7080, so that a phone can reach it
// without a certificate. Production signalling does not.
/**
 * Fetches `<hubUrl><path>` and reads the answer with `read`, all within
 * `timeoutMs`. Rejects with a ConnectError: `rejected` for an HTTP error (its
 * text, cut to 200 characters, in the message) or an answer that `read`
 * cannot read, `timeout` when the time ran out, `refused` when the request
 * failed.
 */
export async function requestHub<T>(
  hubUrl: string,
  path: string,
  init: RequestInit,
  read: (response: Response) => Promise<T>,
  timeoutMs: number,
): Promise<T> {
  const controller = new AbortController();
  let timedOut = false;
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);
  try {
    const response = await fetch(`${hubUrl}${path}`, { ...init, signal: controller.signal });
    if (!response.ok) {
      const reason = (await response.text()).trim().slice(0, 200);
      throw new ConnectError('rejected', hubUrl, `the hub answered ${response.status}${reason ? `: ${reason}` : ''}`);
    }
    try {
      return await read(response);
    } catch (error) {
      if (timedOut) throw error; // classified below
      throw new ConnectError('rejected', hubUrl, `the answer cannot be read: ${error instanceof Error ? error.message : String(error)}`);
    }
  } catch (error) {
    if (error instanceof ConnectError) throw error;
    if (timedOut) throw new ConnectError('timeout', hubUrl, `no answer within ${timeoutMs / 1000} s`);
    throw new ConnectError('refused', hubUrl, error instanceof Error ? error.message : String(error));
  } finally {
    clearTimeout(timer);
  }
}
