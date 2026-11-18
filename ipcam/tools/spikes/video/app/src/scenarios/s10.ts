// S10, MV12 and MV13: the candidate types of the ICE pair that the connection
// selected (never addresses), and whether a permission prompt appeared, which
// only the person can say: the app asks yes or no. Under autorun nobody can
// answer, and the prompt is n/a.
//
// Notes: `pair: <local>/<remote>` or `pair: n/a`, `prompt: yes|no|n/a`.
import type { ScenarioDefinition } from './runner';

const ANSWER_TIMEOUT_MS = 10 * 60_000;

export const S10: ScenarioDefinition = {
  async run(run) {
    const session = await run.session(0);
    const pair = await session.pair();
    run.note(pair === 'n/a' ? 'pair: n/a' : `pair: ${pair.local}/${pair.remote}`);
    const ask = run.env.ask;
    const answer = ask ? await run.within(ask(run.env.t('s10Question')), ANSWER_TIMEOUT_MS) : 'timeout';
    run.note(`prompt: ${answer === 'timeout' ? 'n/a' : answer ? 'yes' : 'no'}`);
  },
};
