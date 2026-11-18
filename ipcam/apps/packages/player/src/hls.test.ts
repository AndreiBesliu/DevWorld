import { beforeEach, describe, expect, it, vi } from 'vitest';

const constructed = vi.hoisted(() => ({ configs: [] as unknown[] }));

vi.mock('hls.js', () => ({
  default: class FakeHlsJs {
    constructor(config: unknown) {
      constructed.configs.push(config);
    }
  },
}));

import { defaultHlsFactory } from './hls';

beforeEach(() => {
  constructed.configs.length = 0;
});

describe('defaultHlsFactory', () => {
  it('defaultHlsFactory_buildsHlsJsWithoutLowLatencyMode', () => {
    const hls = defaultHlsFactory();

    expect(hls).toBeDefined();
    expect(constructed.configs).toEqual([{ lowLatencyMode: false }]);
  });

  it('defaultHlsFactory_buildsAFreshInstanceEachTime', () => {
    expect(defaultHlsFactory()).not.toBe(defaultHlsFactory());
    expect(constructed.configs).toHaveLength(2);
  });
});
