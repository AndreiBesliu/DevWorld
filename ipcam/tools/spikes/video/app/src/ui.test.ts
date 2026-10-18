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
