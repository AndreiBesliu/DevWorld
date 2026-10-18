// Prints, as one line of JSON, the spike app's state in the Windows shell for
// the smoke test of ipcam-windows.yml: the page address, window.__spikeErrors,
// the results the page holds, the start of the screen text and the raw answer
// of the shell's `spike_metrics` command. It asks WebView2's DevTools port,
// which the environment variable
// WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS=--remote-debugging-port=<port> opens.
//
//   node apps/desktop/ci/page-state.ts <port>

interface DevToolsTarget {
  type: string;
  url: string;
  webSocketDebuggerUrl?: string;
}

const port = process.argv[2] ?? '9222';
const ANSWER_TIMEOUT_MS = 10_000;

// Runs in the page. The command is called through Tauri's internals, so the
// answer is the shell's own, before native-tauri.ts maps it.
const expression = `(async () => {
  let metrics;
  try {
    metrics = await window.__TAURI_INTERNALS__.invoke('spike_metrics');
  } catch (error) {
    metrics = { error: String(error) };
  }
  return JSON.stringify({
    url: location.href,
    errors: window.__spikeErrors ?? null,
    results: (window.__spikeResults ?? []).map((r) => ({
      scenario: r.scenario,
      interrupted: r.interrupted,
      notes: r.notes,
      samples: Object.fromEntries(Object.entries(r.samples).map(([key, values]) => [key, values.length])),
    })),
    metrics,
    screen: document.body.innerText.slice(0, 1200),
  });
})()`;

const targets = (await (await fetch(`http://127.0.0.1:${port}/json`)).json()) as DevToolsTarget[];
const socketUrl = targets.find((target) => target.type === 'page')?.webSocketDebuggerUrl;
if (socketUrl === undefined) throw new Error(`no page among the DevTools targets: ${JSON.stringify(targets)}`);

const socket = new WebSocket(socketUrl);
const answer = await new Promise<string>((resolve, reject) => {
  const timer = setTimeout(() => {
    reject(new Error(`no answer within ${ANSWER_TIMEOUT_MS} ms`));
  }, ANSWER_TIMEOUT_MS);
  socket.addEventListener('open', () => {
    socket.send(
      JSON.stringify({ id: 1, method: 'Runtime.evaluate', params: { expression, awaitPromise: true, returnByValue: true } }),
    );
  });
  socket.addEventListener('message', (event) => {
    clearTimeout(timer);
    resolve(String(event.data));
  });
  socket.addEventListener('error', () => {
    clearTimeout(timer);
    reject(new Error('the DevTools socket failed'));
  });
});
socket.close();

const reply = JSON.parse(answer) as { result?: { result?: { value?: unknown } } };
console.log(reply.result?.result?.value ?? answer);
