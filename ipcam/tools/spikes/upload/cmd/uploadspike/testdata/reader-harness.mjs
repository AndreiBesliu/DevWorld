// Runs the reader page's script (reader.html, rendered by the Go test, path
// in argv[2]) in a Node vm with stand-ins for the DOM, Google Identity
// Services and fetch, and plays one scenario (argv[3]):
//   "in-flight": the page goes to the background while the targets are
//                being fetched and comes back before that fetch ends;
//   "after":     the page goes to the background while the targets are
//                being fetched and comes back after the attempt ended.
// It prints one JSON line: the cells posted, the interruptions posted, and
// whether the page shows its end and its restart line.
/* global process, console, setTimeout */
import fs from "node:fs";
import vm from "node:vm";

const html = fs.readFileSync(process.argv[2], "utf8");
const scenario = process.argv[3];
const script = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => m[1]).join("\n");

const elements = {};
const element = (id) => (elements[id] ??= { id, hidden: true, textContent: "", children: [], listeners: {},
  append(c) { this.children.push(c); }, addEventListener(t, f) { this.listeners[t] = f; } });
const docListeners = {};
const document = {
  hidden: false,
  getElementById: element,
  createElement: () => ({ textContent: "" }),
  addEventListener: (t, f) => { docListeners[t] = f; },
};
const setVisible = (visible) => { document.hidden = !visible; docListeners.visibilitychange(); };

const posted = { cells: [], interruptions: 0 };
let releaseTargets;
let targetCalls = 0;
const targets = [{ creator: "android", folderId: "f-android" }, { creator: "tv", folderId: "f-tv" }];
const answer = (body) => ({ ok: true, json: async () => body, arrayBuffer: async () => new ArrayBuffer(16) });
async function fetch(url, opts = {}) {
  if (url.endsWith("/spike/v1/visibility/targets")) {
    targetCalls++;
    if (targetCalls === 1) {
      return new Promise((resolve) => { releaseTargets = () => resolve(answer(targets)); });
    }
    return answer(targets);
  }
  if (url.endsWith("/spike/v1/visibility/web")) {
    posted.cells.push(JSON.parse(opts.body).creator);
    return { ok: true };
  }
  if (url.endsWith("/spike/v1/visibility/interrupted")) {
    posted.interruptions++;
    return { ok: true };
  }
  if (url.includes("alt=media")) return answer(null);
  const folder = decodeURIComponent(url).match(/'([^']+)' in parents/);
  if (folder) {
    const creator = folder[1].replace("f-", "");
    return answer({ files: [{ id: "id-" + creator, name: "vis-" + creator + ".txt" }] });
  }
  throw new Error("unexpected fetch " + url);
}
const google = { accounts: { oauth2: { initTokenClient: (c) => ({ requestAccessToken: () => c.callback({ access_token: "fake-token" }) }) } } };
const context = vm.createContext({ document, fetch, google, setTimeout: (f) => setTimeout(f, 0), console });
vm.runInContext(script, context);

const settle = () => new Promise((r) => setTimeout(r, 50));
(async () => {
  await element("start").listeners.click();
  await settle();
  setVisible(false);
  if (scenario === "in-flight") {
    setVisible(true);
    await settle();
    releaseTargets();
  } else {
    releaseTargets();
    await settle();
    setVisible(true);
  }
  for (let i = 0; i < 10; i++) await settle();
  console.log(JSON.stringify({ cells: posted.cells, interruptions: posted.interruptions, targetCalls,
    done: !element("done").hidden, resumed: !element("resumed").hidden }));
})().catch((e) => { console.log(JSON.stringify({ error: String(e) })); });
