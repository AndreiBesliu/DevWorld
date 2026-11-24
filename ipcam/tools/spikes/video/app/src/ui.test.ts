import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { createI18n } from './i18n';
import type { InterruptReason, ScenarioResult } from './results';
import { ConnectError } from './session';
import {
  connectErrorLines,
  defaultHubUrl,
  hubStatusText,
  offersRepeat,
  queueEventLines,
  queueOutcomeLines,
  statusText,
  type Status,
} from './ui';

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

describe('the status of a scenario', () => {
  const { t } = createI18n({ lang: 'ro' });
  const result = (over: Partial<ScenarioResult> = {}): ScenarioResult => ({
    scenario: 'S7',
    device: { platform: 'android', ua: 'test' },
    startedAt: '2026-10-19T10:00:00.000Z',
    durationS: 6.4,
    interrupted: false,
    hubJitterMs: [],
    samples: {},
    notes: [],
    ...over,
  });

  it('statusText_notRunHereButCompleteInSpikehub_isDone', () => {
    // Chrome's runs of 2026-11-17 (v0.1.0) show as done, and the automatic run skips them.
    expect(statusText({ kind: 'idle' }, t, true)).toBe('gata (rezultatul e în spikehub)');
    expect(statusText({ kind: 'idle' }, t, false)).toBe('neînceput');
    expect(statusText({ kind: 'running' }, t, true)).toBe(t('statusRunning'));
  });

  it('statusText_finishedRun_isDone', () => {
    const status: Status = { kind: 'done', result: result() };

    expect(statusText(status, t)).toBe('gata în 6 s');
    expect(offersRepeat(status)).toBe(false);
  });

  it('statusText_runWithAFailedNote_isFailedWithItsReasonAndOffersRepeat', () => {
    // client-failed-shown-done: the report drops such a run, so the screen must not say «gata».
    const status: Status = { kind: 'done', result: result({ notes: ['wake lock: n/a', 'failed: Error: no track for this slot'] }) };

    expect(statusText(status, t)).toBe('eșuat: no track for this slot');
    expect(offersRepeat(status)).toBe(true);
  });

  it('statusText_failedReason_isShortened', () => {
    const long = 'x'.repeat(300);
    const status: Status = { kind: 'done', result: result({ notes: [`failed: BindError: ${long}`] }) };

    expect(statusText(status, t).length).toBeLessThanOrEqual('eșuat: '.length + 120);
    expect(statusText(status, t)).toMatch(/^eșuat: x+…$/);
  });

  it('statusText_thrownError_isFailed', () => {
    expect(statusText({ kind: 'failed', detail: 'refused' }, t)).toBe('eșuat: refused');
  });

  it.each([
    [undefined, 'întrerupt: ecranul s-a stins sau aplicația a trecut în fundal, deci rezultatul nu intră în verdict. Repetați scenariul.'],
    [{ kind: 'hidden', atS: 12 }, 'întrerupt: ecranul s-a stins sau aplicația a trecut în fundal, deci rezultatul nu intră în verdict. Repetați scenariul.'],
    [
      { kind: 'session-lost', atS: 300, how: 'connection failed' },
      'întrerupt: conexiunea cu hub-ul s-a pierdut. Verificați fereastra spikehub și apăsați «Repetă».',
    ],
    [{ kind: 'charging', atS: 240 }, 'întrerupt: telefonul e la încărcător. Scoateți-l din încărcător și apăsați «Repetă».'],
    [
      { kind: 'early-return', backgroundS: 100 },
      'întrerupt: v-ați întors după 1 min 40 s; e nevoie de cel puțin 2 minute. Apăsați «Repetă».',
    ],
    [{ kind: 'early-return', backgroundS: 45 }, 'întrerupt: v-ați întors după 45 s; e nevoie de cel puțin 2 minute. Apăsați «Repetă».'],
    [
      { kind: 'pip-left', pipS: 290 },
      'întrerupt: fereastra mică s-a închis după 4 min 50 s; e nevoie de 5 minute. Apăsați «Repetă».',
    ],
  ] as const)('statusText_interrupted_saysWhy (%j)', (interruption, text) => {
    const status: Status = { kind: 'done', result: result({ interrupted: true, ...(interruption ? { interruption } : {}) }) };

    expect(statusText(status, t)).toBe(text);
    expect(offersRepeat(status)).toBe(true);
  });

  // During the automatic run the queue starts an interrupted scenario again by
  // itself (its row's button is disabled), and after a stop its line says to
  // press «Rulează tot pentru acest dispozitiv»: the row says why, never «Repetă».
  it.each([
    [undefined, 'întrerupt: fereastra nu mai era vizibilă.'],
    [{ kind: 'hidden', atS: 12 }, 'întrerupt: fereastra nu mai era vizibilă.'],
    [{ kind: 'session-lost', atS: 300, how: 'connection failed' }, 'întrerupt: conexiunea cu hub-ul s-a pierdut.'],
    [{ kind: 'charging', atS: 240 }, 'întrerupt: telefonul era la încărcător.'],
    [{ kind: 'early-return', backgroundS: 45 }, 'întrerupt: v-ați întors după 45 s, iar S5 are nevoie de cel puțin 2 minute.'],
    [{ kind: 'pip-left', pipS: 290 }, 'întrerupt: fereastra mică s-a închis după 4 min 50 s, iar S6 are nevoie de 5 minute.'],
  ] as const)('statusText_interruptedInTheAutomaticRun_saysWhyWithoutRepeat (%j)', (interruption, text) => {
    const status: Status = {
      kind: 'done',
      result: result({ interrupted: true, durationS: 12, ...(interruption ? { interruption } : {}) }),
      queued: true,
    };

    expect(statusText(status, t)).toBe(text);
    expect(statusText(status, t)).not.toMatch(/Repet/);
    expect(statusText(status, createI18n({ lang: 'en' }).t)).not.toMatch(/Repeat/);
  });

  it('statusText_notStarted_saysWhat', () => {
    // R48: S3 on a phone on the charger does not start.
    const status: Status = { kind: 'refused', key: 's3Charging' };

    expect(statusText(status, t)).toBe('Scoateți telefonul din încărcător înainte de S3.');
    expect(offersRepeat(status)).toBe(false);
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

describe('the automatic run on the page', () => {
  const { t } = createI18n({ lang: 'ro' });

  it('autoRun_bannerSaysWhatHappenedAndThatTheRunContinued', () => {
    expect(queueEventLines({ kind: 'resumed', id: 'S3-9', count: 1, reason: { kind: 'hidden', atS: 40 } }, t)).toEqual([
      'S3-9 s-a întrerupt (1 din 3): fereastra nu mai era vizibilă. A pornit din nou de la început, iar rularea continuă.',
    ]);
    expect(queueEventLines({ kind: 'retry', id: 'S2' }, t)).toEqual(['S2 a eșuat și pornește încă o dată; rularea continuă.']);
  });

  // Requirement 5: the banner says what happened, whatever interrupted the scenario.
  const reasons: [string, InterruptReason, string][] = [
    ['hidden', { kind: 'hidden', atS: 40 }, 'fereastra nu mai era vizibilă'],
    ['session-lost', { kind: 'session-lost', atS: 9, how: 'connection failed' }, 'conexiunea cu hub-ul s-a pierdut'],
    ['charging', { kind: 'charging', atS: 61 }, 'telefonul era la încărcător'],
    ['early-return', { kind: 'early-return', backgroundS: 100 }, 'v-ați întors după 1 min 40 s, iar S5 are nevoie de cel puțin 2 minute'],
    ['pip-left', { kind: 'pip-left', pipS: 130 }, 'fereastra mică s-a închis după 2 min 10 s, iar S6 are nevoie de 5 minute'],
  ];

  it.each(reasons)('autoRun_interruptedBanner_%s_saysWhy', (_kind, reason, why) => {
    expect(queueEventLines({ kind: 'resumed', id: 'S9', count: 2, reason }, t)).toEqual([
      `S9 s-a întrerupt (2 din 3): ${why}. A pornit din nou de la început, iar rularea continuă.`,
    ]);
    expect(queueEventLines({ kind: 'interrupted', id: 'S9', count: 2, reason }, t)).toEqual([
      `S9 s-a întrerupt: ${why}. Rularea continuă de îndată ce fereastra e vizibilă.`,
    ]);
  });

  it('autoRun_interruptedBanner_onlyAHiddenWindowBlamesTheWindow', () => {
    for (const [kind, reason] of reasons) {
      const [line] = queueEventLines({ kind: 'resumed', id: 'S9', count: 1, reason }, t) ?? [];
      expect(line?.includes('fereastra nu mai era vizibilă'), kind).toBe(kind === 'hidden');
    }
  });

  it('autoRun_pauseIsCountedDown', () => {
    expect(queueEventLines({ kind: 'pause', leftMs: 581_000 }, t)).toEqual([
      'Pauză de răcire: mai sunt 9 min 41 s. Lăsați telefonul pe masă, scos din încărcător; rularea continuă singură.',
    ]);
  });

  it('autoRun_coldStartIsCountedDown', () => {
    expect(queueEventLines({ kind: 'cold', id: 'S3-9', leftMs: 300_000 }, t)).toEqual([
      'Înainte de S3-9, telefonul stă 10 minute fără încărcător, ca să nu fie cald: mai sunt 5 min. ' +
        'Lăsați-l pe masă, scos din încărcător; rularea continuă singură.',
    ]);
  });

  it('autoRun_scenarioLine', () => {
    expect(queueEventLines({ kind: 'scenario', id: 'S7', index: 1, total: 2, attempt: 1 }, t)).toEqual(['Rulează S7 (1 din 2).']);
  });

  it('autoRun_threeInterruptionsStopWithAClearLine', () => {
    expect(queueOutcomeLines({ kind: 'interrupted-too-often', id: 'S4', reason: { kind: 'hidden', atS: 600 } }, t)).toEqual([
      'Rularea s-a oprit: S4 s-a întrerupt de 3 ori, ultima dată pentru că fereastra nu mai era vizibilă.',
      'Lăsați fereastra vizibilă și apăsați din nou «Rulează tot pentru acest dispozitiv»; ce e gata nu se mai rulează.',
    ]);
  });

  it.each([
    [
      'session-lost',
      { kind: 'session-lost', atS: 9, how: 'connection failed' },
      'Rularea s-a oprit: S9 s-a întrerupt de 3 ori, ultima dată pentru că conexiunea cu hub-ul s-a pierdut.',
      'Verificați că fereastra spikehub e deschisă și că rețeaua merge, apoi apăsați din nou «Rulează tot pentru acest dispozitiv»; ce e gata nu se mai rulează.',
    ],
    [
      'charging',
      { kind: 'charging', atS: 61 },
      'Rularea s-a oprit: S9 s-a întrerupt de 3 ori, ultima dată pentru că telefonul era la încărcător.',
      'Scoateți telefonul din încărcător și apăsați din nou «Rulează tot pentru acest dispozitiv»; ce e gata nu se mai rulează.',
    ],
    [
      'early-return',
      { kind: 'early-return', backgroundS: 45 },
      'Rularea s-a oprit: S9 s-a întrerupt de 3 ori, ultima dată pentru că v-ați întors după 45 s, iar S5 are nevoie de cel puțin 2 minute.',
      'Apăsați din nou «Rulează tot pentru acest dispozitiv» și urmați instrucțiunea de pe ecran până la capăt; ce e gata nu se mai rulează.',
    ],
    [
      'pip-left',
      { kind: 'pip-left', pipS: 200 },
      'Rularea s-a oprit: S9 s-a întrerupt de 3 ori, ultima dată pentru că fereastra mică s-a închis după 3 min 20 s, iar S6 are nevoie de 5 minute.',
      'Apăsați din nou «Rulează tot pentru acest dispozitiv» și urmați instrucțiunea de pe ecran până la capăt; ce e gata nu se mai rulează.',
    ],
  ] satisfies [string, InterruptReason, string, string][])('autoRun_threeInterruptions_%s_stopLineSaysWhyAndWhatToDo', (_kind, reason, stop, advice) => {
    expect(queueOutcomeLines({ kind: 'interrupted-too-often', id: 'S9', reason }, t)).toEqual([stop, advice]);
  });

  it('autoRun_doneAndNothingLeft', () => {
    expect(queueOutcomeLines({ kind: 'done', ran: ['S7', 'S9'], skipped: ['S10'], failed: [] }, t)).toEqual([
      'Gata: toate scenariile acestui dispozitiv au rezultate.',
    ]);
    expect(queueOutcomeLines({ kind: 'done', ran: ['S7', 'S9'], skipped: [], failed: ['S9'] }, t)).toEqual([
      'Gata: toate scenariile acestui dispozitiv au rezultate.',
      'Au eșuat de două ori: S9. Faceți o captură de ecran și trimiteți-ne-o în chat.',
    ]);
    expect(queueOutcomeLines({ kind: 'done', ran: [], skipped: ['S10'], failed: [] }, t)).toEqual([
      'Totul e gata pe acest dispozitiv. «Rulează din nou tot» reia toate scenariile.',
    ]);
  });

  it('preflight_noH264_showsTheGuidesOpenH264Steps', () => {
    const lines = queueOutcomeLines({ kind: 'no-h264' }, t);

    expect(lines[0]).toBe('Firefox nu afișează încă H.264, pentru că îi lipsește pluginul OpenH264. Rularea nu a pornit.');
    expect(lines.join('\n')).toContain('„Extensii și teme” → „Pluginuri”');
    expect(lines.join('\n')).toContain('furnizat de Cisco Systems');
    expect(lines.join('\n')).toContain('lăsați Firefox deschis 5 minute, reporniți-l');
  });

  it('autoRun_adviceBeforeTheRun', () => {
    expect(t('advicePc')).toBe(
      'Cât rulează, lăsați fereastra vizibilă (jumătate de ecran e de ajuns: Windows + ←) și nu blocați PC-ul.',
    );
    expect(t('runAll')).toBe('Rulează tot pentru acest dispozitiv');
    expect(t('rerunAll')).toBe('Rulează din nou tot');
  });
});

describe('the hub panel (Windows shell)', () => {
  it('hubStatus_saysWhatToDo', () => {
    const { t } = createI18n({ lang: 'ro' });
    expect(hubStatusText({ kind: 'running', listening: true }, t)).toBe('Hub-ul rulează. Ce scrie el apare mai jos.');
    expect(hubStatusText({ kind: 'ended', code: 1 }, t)).toBe(
      'Hub-ul s-a oprit singur (cod 1). Motivul e în ultimele rânduri de mai jos.',
    );
    expect(hubStatusText({ kind: 'ended', code: null }, t)).toContain('(cod ?)');
    expect(hubStatusText({ kind: 'missing' }, t)).toContain('nu este spikehub.exe');
    expect(hubStatusText({ kind: 'failed', detail: '' }, t)).toBe('Scrieți folderul spikehub, apoi apăsați «Pornește hub-ul».');
    expect(hubStatusText({ kind: 'failed', detail: 'os error 267' }, t)).toBe('Hub-ul nu poate porni din acest folder: os error 267');
    const en = createI18n({ lang: 'en' }).t;
    expect(hubStatusText({ kind: 'stopped' }, en)).toBe('The hub is stopped.');
  });
});
