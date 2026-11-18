// Runs the setup page's script (setup.html, rendered by the Go test, path
// in argv[2]) in a Node vm with stand-ins for the DOM, and drops on it the
// client JSON files of argv[3] (a JSON array of {name, text}), all at once,
// as the owner does. It prints one JSON line: every field's value after
// the drop.
/* global process, console */
import fs from "node:fs";
import vm from "node:vm";

const html = fs.readFileSync(process.argv[2], "utf8");
const files = JSON.parse(process.argv[3]);
const script = [...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => m[1]).join("\n");

const names = [...html.matchAll(/name="([a-z]+-(?:id|secret))"/g)].map((m) => m[1]);
const fields = Object.fromEntries(names.map((n) => [n, { value: "" }]));
const elements = {};
const element = (id) => (elements[id] ??= { id, textContent: "", children: [], listeners: {},
  append(c) { this.children.push(c); }, addEventListener(t, f) { this.listeners[t] = f; } });
const document = {
  getElementById: element,
  createElement: () => ({ textContent: "" }),
  querySelectorAll: () => [],
  querySelector: (sel) => fields[sel.match(/\[name='([^']+)'\]/)[1]] ?? null,
};
const context = vm.createContext({ document, console, fetch: async () => ({ ok: true, json: async () => ({}) }) });
vm.runInContext(script, context);
await element("files").listeners.change({ target: { files: files.map((f) => ({ name: f.name, text: async () => f.text })) } });
const values = Object.fromEntries(Object.entries(fields).map(([n, f]) => [n, f.value]));
console.log(JSON.stringify({ values, detected: element("detected").children.map((c) => c.textContent) }));
