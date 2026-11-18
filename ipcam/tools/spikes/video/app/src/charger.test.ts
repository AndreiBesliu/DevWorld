import { describe, expect, it } from 'vitest';
import { watchCharger } from './charger';

describe('the charger as the app has seen it', () => {
  it('charger_neverRead_isUnknown', () => {
    const watch = watchCharger(() => 0);

    watch.see(undefined); // the shell could not tell

    expect(watch.state()).toBeUndefined();
  });

  it('charger_offSinceTheFirstReadingOffIt', () => {
    let now = 1000;
    const watch = watchCharger(() => now);

    watch.see(false);
    now += 60_000;
    watch.see(false); // still off: the count goes on from the first reading
    now += 240_000;

    expect(watch.state()).toEqual({ kind: 'off', forMs: 300_000 });
  });

  it('charger_onThenOff_countsFromTheFirstReadingOffIt', () => {
    let now = 0;
    const watch = watchCharger(() => now);

    watch.see(true);
    expect(watch.state()).toEqual({ kind: 'on' });
    now = 120_000;
    watch.see(undefined); // a reading without the charger changes nothing
    expect(watch.state()).toEqual({ kind: 'on' });
    watch.see(false);
    now = 180_000;

    expect(watch.state()).toEqual({ kind: 'off', forMs: 60_000 });
  });
});
