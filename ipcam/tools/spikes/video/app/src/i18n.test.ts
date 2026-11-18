import { describe, expect, it } from 'vitest';
import { createI18n, LANG_KEY, MESSAGES, minutesAndSeconds } from './i18n';

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
    expect(i18n.t('s5Guide', { time: '14:32' })).toBe('Apăsați butonul Acasă și reveniți după ora 14:32 (cel puțin 2 minute).');

    i18n.setLang('en');
    expect(i18n.lang()).toBe('en');
    expect(i18n.t('start')).toBe('Start');
    expect(i18n.t('s5Guide', { time: '14:32' })).toBe('Press Home and come back after 14:32 (at least 2 minutes).');
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

  it('i18n_ownerWording_integration12', () => {
    const { t } = createI18n({ lang: 'ro' });

    // The term is «codecuri», as in the spec (§16.5) and the report.
    expect(t('descS9')).toBe('Matricea de codecuri: H.265 și H.264 High 5.1 (G9)');
    // The explanation is shown in every shell and browser, not only in Chrome; the camera runs for short windows only (R47).
    expect(t('mv4Explain')).not.toMatch(/Chrome/);
    expect(t('mv4Explain')).toMatch(/cel mult 10 secunde/);
    expect(t('mv4Explain')).not.toMatch(/cât durează testul/);
  });

  it('i18n_mv4Explain_namesThePhonesVerdictOnlyOnThePhone', () => {
    // The capture windows run in every shell and browser, also on the PC, where MV4 decides no
    // verdict: there a refusal costs one measurement of the report, never the phone's verdict.
    for (const lang of ['ro', 'en'] as const) {
      const { t } = createI18n({ lang });
      expect(t('mv4Explain')).not.toMatch(/telefon|phone/i);
      expect(t('mv4ExplainAndroid')).toMatch(lang === 'ro' ? /cel mult 10 secunde/ : /at most 10 seconds/);
    }
    const { t } = createI18n({ lang: 'ro' });
    expect(t('mv4Explain')).toMatch(/cel mult 10 secunde/);
    expect(t('mv4Explain').endsWith(' Dacă refuzați, această măsurătoare lipsește din raport.')).toBe(true);
    // On the phone, refusing costs the phone's verdict, not only one measurement.
    expect(t('mv4ExplainAndroid')).toMatch(/Dacă refuzați, .*verdictul pentru telefon nu se poate da/);
    // Both start alike: the guide quotes their first sentence.
    expect(t('mv4ExplainAndroid').startsWith('Lumina camerei se va aprinde pentru cel mult 10 secunde.')).toBe(true);
    expect(t('mv4Explain').startsWith('Lumina camerei se va aprinde pentru cel mult 10 secunde.')).toBe(true);
  });

  it('minutesAndSeconds_readsLikeTheClock', () => {
    expect(minutesAndSeconds(45)).toBe('45 s');
    expect(minutesAndSeconds(100)).toBe('1 min 40 s');
    expect(minutesAndSeconds(120)).toBe('2 min');
    expect(minutesAndSeconds(290.4)).toBe('4 min 50 s');
  });
});
