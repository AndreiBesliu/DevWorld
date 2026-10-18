// Entry point of the spike app: the start page, and autorun when the page was
// opened with ?autorun=<scenario>&hub=<url> (R5).
import { parseAutorun } from './autorun';
import { startApp } from './ui';

const root = document.getElementById('app');
if (root) {
  const app = startApp(root);
  const request = parseAutorun(location.search, location.origin);
  if (request) void app.autorun(request);
}
