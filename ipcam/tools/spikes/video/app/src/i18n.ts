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
  interruptedSessionLost: 'întrerupt: conexiunea cu hub-ul s-a pierdut. Verificați fereastra spikehub și apăsați «Repetă».',
  interruptedCharging: 'întrerupt: telefonul e la încărcător. Scoateți-l din încărcător și apăsați «Repetă».',
  interruptedEarlyReturn: 'întrerupt: v-ați întors după {time}; e nevoie de cel puțin 2 minute. Apăsați «Repetă».',
  interruptedPipLeft: 'întrerupt: fereastra mică s-a închis după {time}; e nevoie de 5 minute. Apăsați «Repetă».',
  statusFailed: 'eșuat: {detail}',
  s3Charging: 'Scoateți telefonul din încărcător înainte de S3.',
  pendingResults: 'Rezultate netrimise: {n}. Se trimit din nou la fiecare pornire.',
  resend: 'Trimite acum',
  s5Guide: 'Apăsați butonul Acasă și reveniți după ora {time} (cel puțin 2 minute).',
  s10Question: 'A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?',
  yes: 'Da',
  no: 'Nu',
  mv4Explain:
    'Lumina camerei se va aprinde pentru cel mult 10 secunde. Aplicația pornește camera la 1 pixel, ' +
    'pentru că doar așa află dacă video-ul se decodează hardware. ' +
    'Nimic nu se înregistrează și nimic nu se trimite. ' +
    'Dacă refuzați, această măsurătoare lipsește, iar verdictul pentru telefon nu se poate da. Vă rugăm să permiteți.',
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
  descS9: 'Matricea de codecuri: H.265 și H.264 High 5.1 (G9)',
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
  interruptedSessionLost: 'interrupted: the connection to the hub was lost. Check the spikehub window and press «Repeat».',
  interruptedCharging: 'interrupted: the phone is on the charger. Unplug it and press «Repeat».',
  interruptedEarlyReturn: 'interrupted: you came back after {time}; at least 2 minutes are needed. Press «Repeat».',
  interruptedPipLeft: 'interrupted: the small window closed after {time}; 5 minutes are needed. Press «Repeat».',
  statusFailed: 'failed: {detail}',
  s3Charging: 'Unplug the phone from the charger before S3.',
  pendingResults: 'Results not sent yet: {n}. They are sent again at every start.',
  resend: 'Send now',
  s5Guide: 'Press Home and come back after {time} (at least 2 minutes).',
  s10Question: 'Did a permission prompt appear (for the local network, for example)?',
  yes: 'Yes',
  no: 'No',
  mv4Explain:
    'The camera light will turn on for at most 10 seconds. The app starts the camera at 1 pixel, ' +
    'because only then can it tell whether video is decoded in hardware. ' +
    'Nothing is recorded and nothing is sent. ' +
    'If you refuse, this measurement is missing and the phone cannot get a verdict. Please allow it.',
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

/** A duration as the owner reads a clock: `45 s`, `1 min 40 s`, `2 min`. The same in both languages. */
export function minutesAndSeconds(seconds: number): string {
  const total = Math.round(seconds);
  const minutes = Math.floor(total / 60);
  const rest = total % 60;
  if (minutes === 0) return `${rest} s`;
  return rest === 0 ? `${minutes} min` : `${minutes} min ${rest} s`;
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
