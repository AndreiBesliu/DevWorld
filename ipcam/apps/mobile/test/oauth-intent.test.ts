// The spike APK's OAuth redirect (phase0c2 upload spike plan, Task 9a): the
// provider's sign-in page, in a Custom Tab, redirects to the custom scheme
// ro.ipcam.spike, and Android hands that URL to the app's Activity, where
// @capacitor/app's appUrlOpen gives it to the page.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const SRC = join(import.meta.dirname, '../android/app/src');

/** An element of a manifest: its name, attributes and children. */
interface Element {
  name: string;
  attrs: Record<string, string>;
  children: Element[];
}

/** The manifest's elements, without comments; enough XML for a hand-written AndroidManifest.xml. */
function parse(xml: string): Element {
  const root: Element = { name: '#document', attrs: {}, children: [] };
  const stack = [root];
  const source = xml.replace(/<!--[\s\S]*?-->/g, '').replace(/<\?[\s\S]*?\?>/g, '');
  for (const [tag = '', closing = '', name = '', rest = ''] of source.matchAll(/<(\/?)([\w:.-]+)([^>]*)>/g)) {
    const parent = stack.at(-1);
    if (!parent) throw new Error(`unbalanced manifest at ${tag}`);
    if (closing) {
      if (parent.name !== name) throw new Error(`${tag} closes ${parent.name}`);
      stack.pop();
      continue;
    }
    const attrs = Object.fromEntries([...rest.matchAll(/([\w:.-]+)\s*=\s*"([^"]*)"/g)].map(([, key = '', value = '']) => [key, value]));
    const element: Element = { name, attrs, children: [] };
    parent.children.push(element);
    if (!rest.trimEnd().endsWith('/')) stack.push(element);
  }
  if (stack.length !== 1) throw new Error('the manifest has unclosed elements');
  return root;
}

const find = (element: Element, name: string): Element[] => [
  ...element.children.filter((child) => child.name === name),
  ...element.children.flatMap((child) => find(child, name)),
];

const manifest = (flavour: string): Element => parse(readFileSync(join(SRC, flavour, 'AndroidManifest.xml'), 'utf8'));

/** The intent filters of the Activity `.MainActivity` in a manifest. */
const mainActivityFilters = (root: Element): Element[] =>
  find(root, 'activity')
    .filter((activity) => activity.attrs['android:name'] === '.MainActivity')
    .flatMap((activity) => find(activity, 'intent-filter'));

describe('the OAuth redirect into the spike APK', () => {
  it('spikeManifest_hasOAuthIntentFilter', () => {
    const filters = mainActivityFilters(manifest('spike')).map((filter) => ({
      actions: find(filter, 'action').map((action) => action.attrs['android:name']),
      categories: find(filter, 'category')
        .map((category) => category.attrs['android:name'])
        .sort(),
      schemes: find(filter, 'data').map((data) => data.attrs['android:scheme']),
    }));

    // Google's ro.ipcam.spike:/oauth2redirect and OneDrive's ro.ipcam.spike://oauth/callback.
    expect(filters).toContainEqual({
      actions: ['android.intent.action.VIEW'],
      categories: ['android.intent.category.BROWSABLE', 'android.intent.category.DEFAULT'],
      schemes: ['ro.ipcam.spike'],
    });
  });

  it('mainManifest_takesNoOAuthRedirect', () => {
    // The scheme belongs to the spike flavour only: the product's login (Faza 1) decides its own.
    const schemes = find(manifest('main'), 'data').map((data) => data.attrs['android:scheme']);

    expect(schemes).not.toContain('ro.ipcam.spike');
  });

  it('mainActivity_isSingleTask_soTheRedirectReachesTheRunningPage', () => {
    // A VIEW intent to a singleTask Activity goes to the running instance
    // (onNewIntent, then appUrlOpen), not to a new Activity with a new page
    // that knows nothing of the login.
    const [activity] = find(manifest('main'), 'activity').filter((element) => element.attrs['android:name'] === '.MainActivity');

    expect(activity?.attrs['android:launchMode']).toBe('singleTask');
  });
});
