// Drives the spike app's hub panel (SPIKE-ONLY, plan 2026-11-24-spike-hub-panel)
// in the Windows shell for the smoke test of ipcam-windows.yml, through
// WebView2's DevTools port, which windows-smoke.ps1 opens:
//
//   node apps/desktop/ci/page-hub.ts <port> start <folder>   types the folder and presses «Pornește hub-ul»
//   node apps/desktop/ci/page-hub.ts <port> stop             presses «Oprește hub-ul»
//   node apps/desktop/ci/page-hub.ts <port> read             prints the panel as one line of JSON
//
// Every action prints the panel's state afterwards: whether it exists, its
// status line, its log and whether its buttons are enabled.

interface DevToolsTarget {
  type: string;
  webSocketDebuggerUrl?: string;
}

const [port = '9222', action = 'read', folder = ''] = process.argv.slice(2);
const ANSWER_TIMEOUT_MS = 10_000;

if (!['start', 'stop', 'read'].includes(action)) throw new Error(`unknown action ${action}: start <folder>, stop or read`);

// Runs in the page. The input event is not needed: the panel reads the field when its button is pressed.
const press =
  action === 'start'
    ? `const input = document.querySelector('.hub-process input');
       if (input) input.value = ${JSON.stringify(folder)};
       document.querySelector('.hub-start')?.click();`
    : action === 'stop'
      ? `document.querySelector('.hub-stop')?.click();`
      : '';
const expression = `(() => {
  ${press}
  const panel = document.querySelector('.hub-process');
  const start = document.querySelector('.hub-start');
  const stop = document.querySelector('.hub-stop');
  return JSON.stringify({
    panel: panel !== null && panel.isConnected,
    status: document.querySelector('.hub-status')?.textContent ?? null,
    log: document.querySelector('.hub-log')?.textContent ?? null,
    startEnabled: start ? !start.disabled : null,
    stopEnabled: stop ? !stop.disabled : null,
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
// PowerShell decodes a program's output in the console's code page, not UTF-8:
// the panel's Romanian letters go out as JSON escapes, which ConvertFrom-Json reads back.
const text = String(reply.result?.result?.value ?? answer);
console.log(text.replace(/[^\x20-\x7e]/g, (char) => `\\u${char.charCodeAt(0).toString(16).padStart(4, '0')}`));
