import { create } from '@bufbuild/protobuf';
import { DateSchema, UsageDaySchema } from '@husonym/sdk';
import { dayPoints } from './days';
import { periodRange } from './period';

function usageDay(month: number, day: number, rowsRead: number, runs: number) {
  return create(UsageDaySchema, {
    day: create(DateSchema, { year: 2026, month, day }),
    rowsRead: BigInt(rowsRead),
    runs: BigInt(runs),
  });
}

describe('dayPoints', () => {
  it('gives one point per day, in the order received, empty days at zero', () => {
    expect(
      dayPoints([
        usageDay(9, 30, 0, 0),
        usageDay(10, 1, 10, 1),
        usageDay(10, 2, 0, 0),
        usageDay(10, 3, 1_234_567, 2),
      ])
    ).toEqual([
      { key: '2026-9-30', label: 'Sep 30', rowsRead: 0, runs: 0 },
      { key: '2026-10-1', label: 'Oct 1', rowsRead: 10, runs: 1 },
      { key: '2026-10-2', label: 'Oct 2', rowsRead: 0, runs: 0 },
      { key: '2026-10-3', label: 'Oct 3', rowsRead: 1234567, runs: 2 },
    ]);
  });

  it('gives nothing for no day', () => {
    expect(dayPoints([])).toEqual([]);
  });

  it('leaves out a day that tells no date', () => {
    expect(
      dayPoints([
        create(UsageDaySchema, { rowsRead: BigInt(5), runs: BigInt(1) }),
        usageDay(10, 9, 5, 1),
      ]).map((point) => point.label)
    ).toEqual(['Oct 9']);
  });

  it('gives ninety points for ninety days', () => {
    const { fromDay } = periodRange(
      'last-90-days',
      new Date('2026-10-09T12:00:00Z'),
      'UTC'
    );
    const start = Date.UTC(fromDay.year, fromDay.month - 1, fromDay.day);
    const days = Array.from({ length: 90 }, (_, i) => {
      const at = new Date(start + i * 86_400_000);
      return usageDay(at.getUTCMonth() + 1, at.getUTCDate(), 0, 0);
    });
    const points = dayPoints(days);
    expect(points).toHaveLength(90);
    expect(points[0].label).toBe('Jul 12');
    expect(points[89].label).toBe('Oct 9');
    expect(new Set(points.map((point) => point.key)).size).toBe(90);
  });
});
