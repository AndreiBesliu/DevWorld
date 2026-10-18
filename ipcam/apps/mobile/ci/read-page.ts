// Prints the spike app's state from the emulator's WebView for the emulator job
// of ipcam-mobile.yml: the page address, window.__spikeErrors, the results the
// page holds and the start of the screen text. It asks the WebView's DevTools
// socket, which debug builds open and which
// `adb forward tcp:<port> localabstract:webview_devtools_remote_<pid>` exposes.
//
//   node apps/mobile/ci/read-page.ts <port>

interface DevToolsTarget {
  type: string;
  webSocketDebuggerUrl?: string;
}

const port = process.argv[2] ?? '9222';
const ANSWER_TIMEOUT_MS = 10_000;

const expression = `JSON.stringify({
  url: location.href,
  errors: window.__spikeErrors ?? null,
  results: (window.__spikeResults ?? []).map((r) => ({
    scenario: r.scenario,
    notes: r.notes,
    samples: Object.fromEntries(Object.entries(r.samples).map(([key, values]) => [key, values.length])),
  })),
  screen: document.body.innerText.slice(0, 1500),
})`;

const targets = (await (await fetch(`http://127.0.0.1:${port}/json`)).json()) as DevToolsTarget[];
const socketUrl = targets.find((target) => target.type === 'page')?.webSocketDebuggerUrl;
if (socketUrl === undefined) throw new Error(`no page among the DevTools targets: ${JSON.stringify(targets)}`);

const socket = new WebSocket(socketUrl);
const answer = await new Promise<string>((resolve, reject) => {
  const timer = setTimeout(() => {
    reject(new Error(`no answer within ${ANSWER_TIMEOUT_MS} ms`));
  }, ANSWER_TIMEOUT_MS);
  socket.addEventListener('open', () => {
    socket.send(JSON.stringify({ id: 1, method: 'Runtime.evaluate', params: { expression, returnByValue: true } }));
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
