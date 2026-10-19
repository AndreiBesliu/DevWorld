import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { createI18n } from './i18n';
import { ConnectError } from './session';
import { connectErrorLines, defaultHubUrl } from './ui';

const HUB = 'http://192.168.1.20:7080';

describe('connect errors', () => {
  it('connectError_showsTriedUrlAndHint', () => {
    // Review Focus 1: guest Wi-Fi, AP isolation, another subnet or a firewall on "Public".
    const { t } = createI18n({ lang: 'ro' });

    const timeout = connectErrorLines(new ConnectError('timeout', HUB, 'no answer within 10 s'), t);

    expect(timeout).toContain(`Adresa încercată: ${HUB}`);
    expect(timeout.join('\n')).toContain('timeout');
    expect(timeout.join('\n')).toContain(
      'același Wi-Fi, nu rețeaua pentru oaspeți; permiteți spikehub în firewall la «Rețele private»',
    );

    const refused = connectErrorLines(new ConnectError('refused', HUB, 'Failed to fetch'), t).join('\n');
    expect(refused).toContain('refused');
    expect(refused).toContain(HUB);
    expect(refused).toContain('«Rețele private»');
  });

  it('connectError_rejected_showsWhatTheHubSaid', () => {
    const { t } = createI18n({ lang: 'en' });

    const lines = connectErrorLines(new ConnectError('rejected', HUB, 'the hub answered 403: origin not allowed'), t);

    expect(lines.join('\n')).toContain('the hub answered 403: origin not allowed');
    expect(lines).toContain(`Address tried: ${HUB}`);
  });

  it('connectError_otherError_showsItsMessage', () => {
    const { t } = createI18n({ lang: 'ro' });

    expect(connectErrorLines(new Error('SESSION_CLOSED'), t).join('\n')).toContain('SESSION_CLOSED');
  });
});

describe('the hub address field', () => {
  it('defaultHubUrl_webUsesTheOrigin_shellsRememberTheLastOne', () => {
    expect(defaultHubUrl('chrome', 'http://localhost:7080', null)).toBe('http://localhost:7080');
    expect(defaultHubUrl('firefox', 'http://192.168.1.20:7080', 'http://10.0.0.9:7080')).toBe('http://192.168.1.20:7080');
    // Capacitor serves the app from http://localhost and Tauri from tauri.localhost: neither is the hub.
    expect(defaultHubUrl('android', 'http://localhost', 'http://192.168.1.20:7080')).toBe('http://192.168.1.20:7080');
    expect(defaultHubUrl('windows-tauri', 'http://tauri.localhost', null)).toBe('');
  });
});

describe('the stage on the page', () => {
  /** The declarations of each rule in the page's style sheet, by selector. */
  function pageStyles(): Map<string, Map<string, string>> {
    const html = readFileSync(new URL('../index.html', import.meta.url), 'utf8');
    const css = /<style>([\s\S]*?)<\/style>/.exec(html)?.[1] ?? '';
    const rules = new Map<string, Map<string, string>>();
    for (const [, selector = '', body = ''] of css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
      const declarations = new Map<string, string>();
      for (const declaration of body.split(';')) {
        const [property = '', ...value] = declaration.split(':');
        if (property.trim() !== '') declarations.set(property.trim(), value.join(':').trim());
      }
      rules.set(selector.trim(), declarations);
    }
    return rules;
  }

  it('stage_duringARun_coversTheViewportOverTheControls', () => {
    // client-stage-offscreen: the tiles, S5's instruction and S10's question were below the fold.
    const styles = pageStyles();
    const idle = styles.get('.stage');
    const active = styles.get('.stage.active');

    expect(idle?.get('display')).toBe('none'); // no layer outside a run
    expect(active?.get('position')).toBe('fixed');
    expect(active?.get('inset')).toBe('0');
    expect(Number(active?.get('z-index'))).toBeGreaterThan(0);
    expect(active?.get('background')).toBe('Canvas'); // opaque: the controls do not show through
    expect(active?.get('display')).toBe('flex');
    expect(active?.get('flex-direction')).toBe('column');
    // The grid takes the rest of the height and every tile fits in it.
    const grid = styles.get('.stage .grid');
    expect(grid?.get('flex')).toBe('1');
    expect(grid?.get('min-height')).toBe('0');
    expect(grid?.get('grid-template-rows')).toBe('repeat(var(--rows, 1), minmax(0, 1fr))');
    const video = styles.get('.stage video');
    expect(video?.get('height')).toBe('100%');
    expect(video?.get('object-fit')).toBe('contain');
  });
});
