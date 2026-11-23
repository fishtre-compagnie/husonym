import { oldestScan } from './pii-scan-cache';

describe('oldestScan', () => {
  it('rend la date la plus ancienne, et rien sans date', () => {
    const a = new Date('2026-11-10T10:00:00Z');
    const b = new Date('2026-11-18T10:00:00Z');
    expect(oldestScan([b, undefined, a])).toEqual(a);
    expect(oldestScan([undefined])).toBeUndefined();
  });
});
