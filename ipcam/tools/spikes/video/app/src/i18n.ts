// The app's texts. Romanian is the owner's language and the default; English is
// one tap away. Romanian is written with ș and ț (comma below), never ş and ţ.
import type { KeyValueStore } from './results';

export type Lang = 'ro' | 'en';

const ro = {
  appTitle: 'Proba video ipcam (Faza 0 b)',
  otherLanguage: 'English',
  hubLabel: 'Adresa hub-ului (o vedeți în fereastra spikehub)',
  check: 'Verifică conexiunea',
  checking: 'Se verifică…',
  connected: 'Conectat: {cameras} camere simulate, perechea ICE {pair}.',
  device: 'Dispozitiv: {device}',
  nativeNa: 'metrici native: n/a',
  connectFailed: 'Nu mă pot conecta la hub.',
  triedUrl: 'Adresa încercată: {url}',
  kindTimeout: 'Hub-ul nu a răspuns la timp (timeout).',
  kindRefused: 'Conexiunea a fost refuzată (refused).',
  kindRejected: 'Hub-ul a refuzat cererea (rejected): {detail}',
  connectHint: 'Verificați: același Wi-Fi, nu rețeaua pentru oaspeți; permiteți spikehub în firewall la «Rețele private».',
  otherError: 'Eroare: {detail}',
  scenarios: 'Scenarii',
  start: 'Pornește',
  repeat: 'Repetă',
  statusIdle: 'neînceput',
  statusRunning: 'rulează… nu închideți ecranul',
  statusRunningShort: 'rulează…',
  statusDone: 'gata în {seconds} s',
  statusInterrupted:
    'întrerupt: ecranul s-a stins sau aplicația a trecut în fundal, deci rezultatul nu intră în verdict. Repetați scenariul.',
  statusFailed: 'eșuat: {detail}',
  pendingResults: 'Rezultate netrimise: {n}. Se trimit din nou la fiecare pornire.',
  resend: 'Trimite acum',
  s5Guide: 'Apăsați butonul Acasă, așteptați 2 minute, reveniți.',
  s10Question: 'A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?',
  yes: 'Da',
  no: 'Nu',
  mv4Explain:
    'Lumina camerei se va aprinde. Aplicația ține camera pornită la 1 pixel cât durează testul, ' +
    'pentru că doar așa Chrome arată dacă video-ul se decodează hardware. ' +
    'Nimic nu se înregistrează și nimic nu se trimite.',
  unsupportedProfile: 'profil video nesuportat',
  h265Unsupported: 'calitate redusă: H.265 nesuportat',
  descS1: 'Pornire: primul cadru, 20 de porniri (MV1)',
  descS2: 'Întârziere glass-to-glass, 100 + 100 de mostre (MV2)',
  'descS3-4': 'Grilă de 4 camere, 10 min, apoi 2 min ecran complet (MV3–MV6)',
  'descS3-9': 'Grilă de 9 camere, 10 min, apoi 2 min ecran complet (MV3–MV6)',
  'descS3-16': 'Grilă de 16 camere, 10 min, apoi 2 min ecran complet (MV3–MV6)',
  descS4: 'Stabilitate: 30 min de paginare și ecran complet (MV7)',
  descS5: 'Fundal 2 minute, apoi revenire (MV9)',
  descS6: 'Picture-in-picture 5 minute, doar pe Android (MV8)',
  descS7: 'Înregistrare HLS: 20 de porniri și 20 de salturi (MV10)',
  descS8: 'Argon2id, 10 rulări (MV15)',
  descS9: 'Matricea de coduri: H.265 și H.264 High 5.1 (G9)',
  descS10: 'Perechea ICE și cererea de permisiune (MV12, MV13)',
} as const;

export type MessageKey = keyof typeof ro;

const en: Record<MessageKey, string> = {
  appTitle: 'ipcam video spike (Phase 0 b)',
  otherLanguage: 'Română',
  hubLabel: 'Hub address (shown in the spikehub window)',
  check: 'Check the connection',
  checking: 'Checking…',
  connected: 'Connected: {cameras} simulated cameras, ICE pair {pair}.',
  device: 'Device: {device}',
  nativeNa: 'native metrics: n/a',
  connectFailed: 'Cannot connect to the hub.',
  triedUrl: 'Address tried: {url}',
  kindTimeout: 'The hub did not answer in time (timeout).',
  kindRefused: 'The connection was refused (refused).',
  kindRejected: 'The hub refused the request (rejected): {detail}',
  connectHint: 'Check: the same Wi-Fi, not the guest network; allow spikehub in the firewall on «Private networks».',
  otherError: 'Error: {detail}',
  scenarios: 'Scenarios',
  start: 'Start',
  repeat: 'Repeat',
  statusIdle: 'not started',
  statusRunning: 'running… keep the screen on',
  statusRunningShort: 'running…',
  statusDone: 'done in {seconds} s',
  statusInterrupted:
    'interrupted: the screen turned off or the app went to the background, so the result is left out of the verdict. Repeat the scenario.',
  statusFailed: 'failed: {detail}',
  pendingResults: 'Results not sent yet: {n}. They are sent again at every start.',
  resend: 'Send now',
  s5Guide: 'Press Home, wait 2 minutes, come back.',
  s10Question: 'Did a permission prompt appear (for the local network, for example)?',
  yes: 'Yes',
  no: 'No',
  mv4Explain:
    'The camera light will turn on. The app holds the camera at 1 pixel while the test runs, ' +
    'because only then does Chrome tell whether video is decoded in hardware. ' +
    'Nothing is recorded and nothing is sent.',
  unsupportedProfile: 'unsupported video profile',
  h265Unsupported: 'reduced quality: H.265 not supported',
  descS1: 'Start: first frame, 20 starts (MV1)',
  descS2: 'Glass-to-glass latency, 100 + 100 samples (MV2)',
  'descS3-4': 'Grid of 4 cameras, 10 min, then 2 min fullscreen (MV3–MV6)',
  'descS3-9': 'Grid of 9 cameras, 10 min, then 2 min fullscreen (MV3–MV6)',
  'descS3-16': 'Grid of 16 cameras, 10 min, then 2 min fullscreen (MV3–MV6)',
  descS4: 'Stability: 30 min of paging and fullscreen (MV7)',
  descS5: 'Background for 2 minutes, then back (MV9)',
  descS6: 'Picture-in-picture for 5 minutes, Android only (MV8)',
  descS7: 'HLS recording: 20 starts and 20 seeks (MV10)',
  descS8: 'Argon2id, 10 runs (MV15)',
  descS9: 'Codec matrix: H.265 and H.264 High 5.1 (G9)',
  descS10: 'ICE pair and permission prompt (MV12, MV13)',
};

export const MESSAGES: Record<Lang, Record<MessageKey, string>> = { ro, en };

/** Where the chosen language is remembered. */
export const LANG_KEY = 'spike.lang';

/** A text in the current language, with `{name}` placeholders filled from `vars`. */
export type Translate = (key: MessageKey, vars?: Record<string, string | number>) => string;

export interface I18n {
  lang(): Lang;
  setLang(lang: Lang): void;
  t: Translate;
}

function readLang(storage: KeyValueStore | undefined): Lang {
  try {
    return storage?.getItem(LANG_KEY) === 'en' ? 'en' : 'ro';
  } catch {
    return 'ro'; // storage blocked: the default
  }
}

/** Romanian unless `lang` says otherwise or `storage` remembers English. */
export function createI18n(opts: { lang?: Lang; storage?: KeyValueStore | undefined } = {}): I18n {
  let current: Lang = opts.lang ?? readLang(opts.storage);
  return {
    lang: () => current,
    setLang(lang) {
      current = lang;
      try {
        opts.storage?.setItem(LANG_KEY, lang);
      } catch {
        // Not remembered, which only costs a tap at the next start.
      }
    },
    t: (key, vars = {}) =>
      MESSAGES[current][key].replace(/\{(\w+)\}/g, (match, name: string) => {
        const value = vars[name];
        return value === undefined ? match : String(value);
      }),
  };
}
