import { describe, expect, it } from 'vitest';
import { createI18n, LANG_KEY, MESSAGES } from './i18n';

class MemoryStore {
  readonly map = new Map<string, string>();
  getItem(key: string): string | null {
    return this.map.get(key) ?? null;
  }
  setItem(key: string, value: string): void {
    this.map.set(key, value);
  }
}

describe('i18n', () => {
  it('i18n_roIsDefault_enAvailable', () => {
    const storage = new MemoryStore();
    const i18n = createI18n({ storage });

    expect(i18n.lang()).toBe('ro');
    expect(i18n.t('start')).toBe('Pornește');
    expect(i18n.t('s5Guide')).toBe('Apăsați butonul Acasă, așteptați 2 minute, reveniți.');

    i18n.setLang('en');
    expect(i18n.lang()).toBe('en');
    expect(i18n.t('start')).toBe('Start');
    expect(i18n.t('s5Guide')).toBe('Press Home, wait 2 minutes, come back.');
    // The choice is remembered for the next launch.
    expect(storage.getItem(LANG_KEY)).toBe('en');
    expect(createI18n({ storage }).lang()).toBe('en');
  });

  it('i18n_everyMessageExistsInBothLanguages', () => {
    expect(Object.keys(MESSAGES.en).sort()).toEqual(Object.keys(MESSAGES.ro).sort());
    for (const lang of ['ro', 'en'] as const) {
      for (const [key, text] of Object.entries(MESSAGES[lang])) expect(text.trim(), `${lang}.${key}`).not.toBe('');
    }
  });

  it('i18n_romanianUsesCommaBelowDiacritics', () => {
    // ş and ţ (cedilla) are Turkish letters; Romanian writes ș and ț.
    const text = Object.values(MESSAGES.ro).join('\n');
    expect(text).not.toMatch(/[şţŞŢ]/);
    expect(text).toMatch(/[șț]/);
  });

  it('i18n_fillsPlaceholders', () => {
    const i18n = createI18n({ lang: 'ro' });

    expect(i18n.t('triedUrl', { url: 'http://192.168.1.20:7080' })).toBe('Adresa încercată: http://192.168.1.20:7080');
  });

  it('i18n_unknownStoredLanguage_fallsBackToRomanian', () => {
    const storage = new MemoryStore();
    storage.setItem(LANG_KEY, 'de');

    expect(createI18n({ storage }).lang()).toBe('ro');
  });
});
