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
  statusDoneInHub: 'gata (rezultatul e în spikehub)',
  statusInterrupted:
    'întrerupt: ecranul s-a stins sau aplicația a trecut în fundal, deci rezultatul nu intră în verdict. Repetați scenariul.',
  interruptedSessionLost: 'întrerupt: conexiunea cu hub-ul s-a pierdut. Verificați fereastra spikehub și apăsați «Repetă».',
  interruptedCharging: 'întrerupt: telefonul e la încărcător. Scoateți-l din încărcător și apăsați «Repetă».',
  interruptedEarlyReturn: 'întrerupt: v-ați întors după {time}; e nevoie de cel puțin 2 minute. Apăsați «Repetă».',
  interruptedPipLeft: 'întrerupt: fereastra mică s-a închis după {time}; e nevoie de 5 minute. Apăsați «Repetă».',
  // An interrupted scenario of the automatic run, which starts it again by itself: {reason} is one of the autoReason… texts.
  statusInterruptedAuto: 'întrerupt: {reason}.',
  statusFailed: 'eșuat: {detail}',
  s3Charging: 'Scoateți telefonul din încărcător înainte de S3.',
  pendingResults: 'Rezultate netrimise: {n}. Se trimit din nou la fiecare pornire.',
  resend: 'Trimite acum',
  s5Guide: 'Apăsați butonul Acasă și reveniți după ora {time} (cel puțin 2 minute).',
  s10Question: 'A apărut o cerere de permisiune (de exemplu pentru rețeaua locală)?',
  yes: 'Da',
  no: 'Nu',
  // MV4's camera note: on the PC paths (browsers, ipcam-desktop) MV4 decides no verdict, so a
  // refusal only leaves the measurement out; on the phone it leaves the phone without a verdict.
  mv4Explain:
    'Lumina camerei se va aprinde pentru cel mult 10 secunde. Aplicația pornește camera la 1 pixel, ' +
    'pentru că doar așa află dacă video-ul se decodează hardware. ' +
    'Nimic nu se înregistrează și nimic nu se trimite. ' +
    'Dacă refuzați, această măsurătoare lipsește din raport.',
  mv4ExplainAndroid:
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
  remainingPc: 'Rămase pe acest PC: {devices}',
  remainingPhone: 'Rămase pe telefon: {devices}',
  remainingNone: 'nimic, totul e gata',
  h264Checking: 'Verificare H.264',
  version: 'versiunea {version}',
  // SPIKE-ONLY (update.ts): the test app's own updates.
  updateAvailable: 'Există o versiune nouă: {version} (aveți {current}).',
  updateNow: 'Actualizează acum',
  updateDownloading: 'Se descarcă actualizarea…',
  updateInstalling: 'Se instalează. Aplicația se închide și pornește din nou singură în câteva secunde.',
  updateConfirm: 'Apăsați «Actualizați» în fereastra Android care s-a deschis.',
  updatePermission:
    'Android cere o permisiune: activați «Permiteți din această sursă» pentru Proba ipcam, reveniți în aplicație și apăsați din nou «Actualizează acum».',
  updateFailed: 'Actualizarea nu a reușit: {detail}. Încercați din nou mai târziu.',
  // SPIKE-ONLY (hubprocess.ts): spikehub started by the Windows shell.
  hubTitle: 'Hub-ul pe acest PC (spikehub)',
  hubFolderLabel: 'Folderul spikehub (cel cu spikehub.exe și folderele media și results)',
  hubStart: 'Pornește hub-ul',
  hubStop: 'Oprește hub-ul',
  hubStarting: 'Hub-ul pornește…',
  hubRunning: 'Hub-ul rulează. Ce scrie el apare mai jos.',
  hubStopping: 'Hub-ul se oprește…',
  hubStopped: 'Hub-ul e oprit.',
  hubEnded: 'Hub-ul s-a oprit singur (cod {code}). Motivul e în ultimele rânduri de mai jos.',
  hubMissing: 'În acest folder nu este spikehub.exe. Scrieți folderul în care ați extras spikehub-windows.zip.',
  hubNoFolder: 'Scrieți folderul spikehub, apoi apăsați «Pornește hub-ul».',
  hubFailed: 'Hub-ul nu poate porni din acest folder: {detail}',
  hubLogEmpty: '(hub-ul nu a scris încă nimic)',
  autoTitle: 'Rulare automată',
  runAll: 'Rulează tot pentru acest dispozitiv',
  rerunAll: 'Rulează din nou tot',
  autoDevice: 'Acest dispozitiv: {label}. Scenariile, în ordine: {scenarios}.',
  autoUnknown:
    'Rularea automată nu e disponibilă aici: aplicația nu recunoaște acest dispozitiv. Ea rulează în Chrome, Edge și Firefox ' +
    'pe Windows, în ipcam-desktop și în aplicația «Proba ipcam» de pe telefon.',
  advicePc: 'Cât rulează, lăsați fereastra vizibilă (jumătate de ecran e de ajuns: Windows + ←) și nu blocați PC-ul.',
  advicePhone:
    'Cât rulează, lăsați telefonul pe masă, cu aplicația deschisă și scos din încărcător. La S5 și S6 aplicația vă cheamă cu un sunet.',
  autoStarting: 'Se citesc rezultatele din spikehub…',
  autoPreflight: 'Se verifică dacă Firefox afișează H.264…',
  autoCamera: 'Aplicația cere acum, o singură dată, acces la cameră, pentru S3. Permiteți; apoi rularea merge singură.',
  autoScenario: 'Rulează {id} ({index} din {total}).',
  // The automatic run's lines for an interrupted scenario: {reason} is one of the autoReason… texts.
  autoInterrupted: '{id} s-a întrerupt: {reason}. Rularea continuă de îndată ce fereastra e vizibilă.',
  autoResumed: '{id} s-a întrerupt ({count} din 3): {reason}. A pornit din nou de la început, iar rularea continuă.',
  autoReasonHidden: 'fereastra nu mai era vizibilă',
  autoReasonSessionLost: 'conexiunea cu hub-ul s-a pierdut',
  autoReasonCharging: 'telefonul era la încărcător',
  autoReasonEarlyReturn: 'v-ați întors după {time}, iar S5 are nevoie de cel puțin 2 minute',
  autoReasonPipLeft: 'fereastra mică s-a închis după {time}, iar S6 are nevoie de 5 minute',
  autoRetry: '{id} a eșuat și pornește încă o dată; rularea continuă.',
  autoPause: 'Pauză de răcire: mai sunt {time}. Lăsați telefonul pe masă, scos din încărcător; rularea continuă singură.',
  autoCold:
    'Înainte de {id}, telefonul stă 10 minute fără încărcător, ca să nu fie cald: mai sunt {time}. ' +
    'Lăsați-l pe masă, scos din încărcător; rularea continuă singură.',
  autoAttention: '{id} are nevoie de dvs.: urmați instrucțiunea de pe ecran.',
  s6Guide:
    'S6 are nevoie de dvs. După «Continuă», aplicația trece singură într-o fereastră mică. Nu o atingeți. Când S6 se termină, ' +
    'după 5 minute, telefonul sună: atunci atingeți fereastra mică și pictograma de mărire, ca să reveniți în aplicație.',
  continue: 'Continuă',
  autoPip: 'S6 s-a terminat. Atingeți fereastra mică și pictograma de mărire, ca să reveniți în aplicație; rularea continuă apoi.',
  autoDone: 'Gata: toate scenariile acestui dispozitiv au rezultate.',
  autoDoneFailed: 'Au eșuat de două ori: {ids}. Faceți o captură de ecran și trimiteți-ne-o în chat.',
  autoNothingLeft: 'Totul e gata pe acest dispozitiv. «Rulează din nou tot» reia toate scenariile.',
  autoTooManyInterruptions: 'Rularea s-a oprit: {id} s-a întrerupt de 3 ori, ultima dată pentru că {reason}.',
  autoAgainHidden: 'Lăsați fereastra vizibilă și apăsați din nou «Rulează tot pentru acest dispozitiv»; ce e gata nu se mai rulează.',
  autoAgainSessionLost:
    'Verificați că fereastra spikehub e deschisă și că rețeaua merge, apoi apăsați din nou ' +
    '«Rulează tot pentru acest dispozitiv»; ce e gata nu se mai rulează.',
  autoAgainCharging:
    'Scoateți telefonul din încărcător și apăsați din nou «Rulează tot pentru acest dispozitiv»; ce e gata nu se mai rulează.',
  autoAgainPerson:
    'Apăsați din nou «Rulează tot pentru acest dispozitiv» și urmați instrucțiunea de pe ecran până la capăt; ' +
    'ce e gata nu se mai rulează.',
  autoStopped: 'Rularea automată s-a oprit. Ce e gata nu se mai rulează la următoarea apăsare.',
  openh264: 'Firefox nu afișează încă H.264, pentru că îi lipsește pluginul OpenH264. Rularea nu a pornit.',
  openh264Step1:
    'Meniul ☰ → „Setări” → jos, în stânga, „Extensii și teme” → „Pluginuri”. Trebuie să apară pluginul OpenH264, furnizat de Cisco Systems.',
  openh264Step2: 'Dacă apare ca dezactivat, deschideți meniul lui („…”) și alegeți să fie activat întotdeauna.',
  openh264Step3:
    'Dacă pluginul lipsește, lăsați Firefox deschis 5 minute, reporniți-l și apăsați din nou «Rulează tot pentru acest dispozitiv».',
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
  statusDoneInHub: 'done (the result is in spikehub)',
  statusInterrupted:
    'interrupted: the screen turned off or the app went to the background, so the result is left out of the verdict. Repeat the scenario.',
  interruptedSessionLost: 'interrupted: the connection to the hub was lost. Check the spikehub window and press «Repeat».',
  interruptedCharging: 'interrupted: the phone is on the charger. Unplug it and press «Repeat».',
  interruptedEarlyReturn: 'interrupted: you came back after {time}; at least 2 minutes are needed. Press «Repeat».',
  interruptedPipLeft: 'interrupted: the small window closed after {time}; 5 minutes are needed. Press «Repeat».',
  statusInterruptedAuto: 'interrupted: {reason}.',
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
    'If you refuse, this measurement is missing from the report.',
  mv4ExplainAndroid:
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
  remainingPc: 'Left on this PC: {devices}',
  remainingPhone: 'Left on the phone: {devices}',
  remainingNone: 'nothing, all done',
  h264Checking: 'H.264 check',
  version: 'version {version}',
  updateAvailable: 'A new version is available: {version} (you have {current}).',
  updateNow: 'Update now',
  updateDownloading: 'Downloading the update…',
  updateInstalling: 'Installing. The app closes and starts again by itself in a few seconds.',
  updateConfirm: 'Tap «Update» in the Android window that opened.',
  updatePermission:
    'Android asks for a permission: turn on «Allow from this source» for Proba ipcam, come back to the app and tap «Update now» again.',
  updateFailed: 'The update did not work: {detail}. Try again later.',
  hubTitle: 'The hub on this PC (spikehub)',
  hubFolderLabel: 'The spikehub folder (the one with spikehub.exe and the media and results folders)',
  hubStart: 'Start the hub',
  hubStop: 'Stop the hub',
  hubStarting: 'The hub is starting…',
  hubRunning: 'The hub runs. What it writes shows below.',
  hubStopping: 'The hub is stopping…',
  hubStopped: 'The hub is stopped.',
  hubEnded: 'The hub stopped by itself (code {code}). The reason is in the last lines below.',
  hubMissing: 'This folder has no spikehub.exe. Type the folder where you extracted spikehub-windows.zip.',
  hubNoFolder: 'Type the spikehub folder, then press «Start the hub».',
  hubFailed: 'The hub cannot start from this folder: {detail}',
  hubLogEmpty: '(the hub has not written anything yet)',
  autoTitle: 'Automatic run',
  runAll: 'Run everything for this device',
  rerunAll: 'Run everything again',
  autoDevice: 'This device: {label}. The scenarios, in order: {scenarios}.',
  autoUnknown:
    'The automatic run is not available here: the app does not know this device. It runs in Chrome, Edge and Firefox ' +
    'on Windows, in ipcam-desktop and in the «Proba ipcam» app on the phone.',
  advicePc: 'While it runs, keep the window visible (half the screen is enough: Windows + ←) and do not lock the PC.',
  advicePhone: 'While it runs, leave the phone on the table, with the app open and off the charger. At S5 and S6 the app calls you with a sound.',
  autoStarting: 'Reading the results from spikehub…',
  autoPreflight: 'Checking that Firefox shows H.264…',
  autoCamera: 'The app now asks, once, for the camera, for S3. Allow it; then the run goes on by itself.',
  autoScenario: 'Running {id} ({index} of {total}).',
  autoInterrupted: '{id} was interrupted: {reason}. The run goes on as soon as the window is visible.',
  autoResumed: '{id} was interrupted ({count} of 3): {reason}. It started again from the beginning, and the run goes on.',
  autoReasonHidden: 'the window was not visible',
  autoReasonSessionLost: 'the connection to the hub was lost',
  autoReasonCharging: 'the phone was on the charger',
  autoReasonEarlyReturn: 'you came back after {time}, and S5 needs at least 2 minutes',
  autoReasonPipLeft: 'the small window closed after {time}, and S6 needs 5 minutes',
  autoRetry: '{id} failed and starts once more; the run goes on.',
  autoPause: 'Cooling pause: {time} left. Leave the phone on the table, off the charger; the run goes on by itself.',
  autoCold:
    'Before {id}, the phone stays off the charger for 10 minutes, so that it is not warm: {time} left. ' +
    'Leave it on the table, off the charger; the run goes on by itself.',
  autoAttention: '{id} needs you: follow the instruction on the screen.',
  s6Guide:
    'S6 needs you. After «Continue», the app goes into a small window by itself. Do not touch it. When S6 ends, ' +
    'after 5 minutes, the phone rings: then tap the small window and the enlarge icon to return to the app.',
  continue: 'Continue',
  autoPip: 'S6 has ended. Tap the small window and the enlarge icon to return to the app; then the run goes on.',
  autoDone: 'Done: every scenario of this device has results.',
  autoDoneFailed: 'Failed twice: {ids}. Take a screenshot and send it to us in the chat.',
  autoNothingLeft: 'Everything is done on this device. «Run everything again» repeats every scenario.',
  autoTooManyInterruptions: 'The run stopped: {id} was interrupted 3 times, the last time because {reason}.',
  autoAgainHidden: 'Keep the window visible and press «Run everything for this device» again; what is done does not run again.',
  autoAgainSessionLost:
    'Check that the spikehub window is open and that the network works, then press «Run everything for this device» again; ' +
    'what is done does not run again.',
  autoAgainCharging: 'Unplug the phone from the charger and press «Run everything for this device» again; what is done does not run again.',
  autoAgainPerson:
    'Press «Run everything for this device» again and follow the instruction on the screen to the end; what is done does not run again.',
  autoStopped: 'The automatic run stopped. What is done does not run again at the next press.',
  openh264: 'Firefox does not show H.264 yet: the OpenH264 plugin is missing. The run did not start.',
  openh264Step1:
    'Menu ☰ → “Settings” → at the bottom left, “Extensions & Themes” → “Plugins”. The OpenH264 plugin, provided by Cisco Systems, must be listed.',
  openh264Step2: 'If it shows as disabled, open its menu (“…”) and choose to always activate it.',
  openh264Step3: 'If the plugin is missing, leave Firefox open for 5 minutes, restart it and press «Run everything for this device» again.',
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
