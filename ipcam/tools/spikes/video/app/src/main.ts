// Entry point of the spike app: the start page, and autorun when the page was
// opened with ?autorun=<scenario>&hub=<url> (R5).
import { parseAutorun, reportAutorunError } from './autorun';
import { installAndroidNative } from './native-android';
import { startApp } from './ui';

// Before the start page reads window.spikeNative.
installAndroidNative();

const root = document.getElementById('app');
if (root) {
  const app = startApp(root);
  const request = parseAutorun(location.search, location.origin);
  if (request && 'kind' in request) reportAutorunError(request);
  else if (request) void app.autorun(request);
}
