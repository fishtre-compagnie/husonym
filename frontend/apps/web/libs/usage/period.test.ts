import {
  DEFAULT_USAGE_PERIOD,
  dayLabel,
  isUsagePeriod,
  periodLabel,
  periodRange,
  rangeLabel,
  USAGE_PERIODS,
} from './period';

function day(year: number, month: number, d: number) {
  return { year, month, day: d };
}

describe('periodRange', () => {
  // 23:30 on October 9 in UTC, 01:30 on October 10 in Paris.
  const now = new Date('2026-10-09T23:30:00Z');

  it('ends today and counts today among the days', () => {
    expect(periodRange('last-7-days', now, 'UTC')).toEqual({
      fromDay: day(2026, 10, 3),
      toDay: day(2026, 10, 9),
    });
    expect(periodRange('last-30-days', now, 'UTC')).toEqual({
      fromDay: day(2026, 9, 10),
      toDay: day(2026, 10, 9),
    });
    expect(periodRange('last-90-days', now, 'UTC')).toEqual({
      fromDay: day(2026, 7, 12),
      toDay: day(2026, 10, 9),
    });
  });

  it('reads the months of the calendar', () => {
    expect(periodRange('this-month', now, 'UTC')).toEqual({
      fromDay: day(2026, 10, 1),
      toDay: day(2026, 10, 9),
    });
    expect(periodRange('last-month', now, 'UTC')).toEqual({
      fromDay: day(2026, 9, 1),
      toDay: day(2026, 9, 30),
    });
  });

  it('is computed on the day of the zone, not on the UTC day', () => {
    expect(periodRange('last-7-days', now, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 10, 4),
      toDay: day(2026, 10, 10),
    });
    expect(periodRange('last-7-days', now, 'America/Chicago').toDay).toEqual(
      day(2026, 10, 9)
    );
    // 20:30 on October 9 in Chicago is already October 10 in UTC.
    const evening = new Date('2026-10-09T20:30:00-05:00');
    expect(
      periodRange('last-7-days', evening, 'America/Chicago').toDay
    ).toEqual(day(2026, 10, 9));
    expect(periodRange('last-7-days', evening, 'UTC').toDay).toEqual(
      day(2026, 10, 10)
    );
  });

  it('keeps its days when Paris moves to summer time', () => {
    // March 29, 2026: the day has 23 hours. 00:30 on March 30, local time.
    const after = new Date('2026-03-29T22:30:00Z');
    expect(periodRange('last-7-days', after, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 3, 24),
      toDay: day(2026, 3, 30),
    });
    // 23:30 on March 28, local time, the eve of the change.
    const before = new Date('2026-03-28T22:30:00Z');
    expect(periodRange('last-7-days', before, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 3, 22),
      toDay: day(2026, 3, 28),
    });
    // 03:30 on March 29, local time: the hour that follows the one that does not exist.
    const during = new Date('2026-03-29T01:30:00Z');
    expect(periodRange('last-30-days', during, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 2, 28),
      toDay: day(2026, 3, 29),
    });
  });

  it('keeps its days when Paris moves back to winter time', () => {
    // October 25, 2026: the day has 25 hours. 23:30 on October 25, local time.
    const late = new Date('2026-10-25T22:30:00Z');
    expect(periodRange('last-7-days', late, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 10, 19),
      toDay: day(2026, 10, 25),
    });
    // One hour later it is October 26.
    const next = new Date('2026-10-25T23:30:00Z');
    expect(periodRange('last-7-days', next, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 10, 20),
      toDay: day(2026, 10, 26),
    });
    // 02:30 local time happens twice that night: both are October 25.
    expect(
      periodRange(
        'last-7-days',
        new Date('2026-10-25T00:30:00Z'),
        'Europe/Paris'
      ).toDay
    ).toEqual(day(2026, 10, 25));
    expect(
      periodRange(
        'last-7-days',
        new Date('2026-10-25T01:30:00Z'),
        'Europe/Paris'
      ).toDay
    ).toEqual(day(2026, 10, 25));
  });

  it('is still in the month at 23:30 on its last day', () => {
    // 23:30 on September 30 in Paris.
    const lastEvening = new Date('2026-09-30T21:30:00Z');
    expect(periodRange('this-month', lastEvening, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 9, 1),
      toDay: day(2026, 9, 30),
    });
    expect(periodRange('last-month', lastEvening, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 8, 1),
      toDay: day(2026, 8, 31),
    });
    // The same moment is 11:30 on October 1 on Kiritimati, fourteen hours ahead.
    expect(
      periodRange('this-month', lastEvening, 'Pacific/Kiritimati')
    ).toEqual({ fromDay: day(2026, 10, 1), toDay: day(2026, 10, 1) });
    expect(
      periodRange('last-month', lastEvening, 'Pacific/Kiritimati')
    ).toEqual({ fromDay: day(2026, 9, 1), toDay: day(2026, 9, 30) });
    expect(
      periodRange('last-7-days', lastEvening, 'Pacific/Kiritimati')
    ).toEqual({ fromDay: day(2026, 9, 25), toDay: day(2026, 10, 1) });
  });

  it('crosses a month', () => {
    const start = new Date('2026-03-02T12:00:00Z');
    expect(periodRange('last-7-days', start, 'UTC')).toEqual({
      fromDay: day(2026, 2, 24),
      toDay: day(2026, 3, 2),
    });
    expect(periodRange('last-month', start, 'UTC')).toEqual({
      fromDay: day(2026, 2, 1),
      toDay: day(2026, 2, 28),
    });
  });

  it('crosses a year', () => {
    // 00:30 on January 1, 2027 in Paris.
    const newYear = new Date('2026-12-31T23:30:00Z');
    expect(periodRange('last-7-days', newYear, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 12, 26),
      toDay: day(2027, 1, 1),
    });
    expect(periodRange('this-month', newYear, 'Europe/Paris')).toEqual({
      fromDay: day(2027, 1, 1),
      toDay: day(2027, 1, 1),
    });
    expect(periodRange('last-month', newYear, 'Europe/Paris')).toEqual({
      fromDay: day(2026, 12, 1),
      toDay: day(2026, 12, 31),
    });
    expect(periodRange('last-month', newYear, 'UTC')).toEqual({
      fromDay: day(2026, 11, 1),
      toDay: day(2026, 11, 30),
    });
  });

  it('knows the leap day', () => {
    expect(
      periodRange('last-month', new Date('2028-03-01T12:00:00Z'), 'UTC')
    ).toEqual({ fromDay: day(2028, 2, 1), toDay: day(2028, 2, 29) });
    expect(
      periodRange('last-7-days', new Date('2028-03-02T12:00:00Z'), 'UTC')
        .fromDay
    ).toEqual(day(2028, 2, 25));
  });

  it('never asks for more days than its name says', () => {
    const lengths = USAGE_PERIODS.map((period) => {
      const { fromDay, toDay } = periodRange(period, now, 'Europe/Paris');
      const from = Date.UTC(fromDay.year, fromDay.month - 1, fromDay.day);
      const to = Date.UTC(toDay.year, toDay.month - 1, toDay.day);
      return (to - from) / 86_400_000 + 1;
    });
    expect(lengths).toEqual([7, 30, 90, 10, 30]);
  });
});

describe('periodLabel', () => {
  it('names every period', () => {
    expect(USAGE_PERIODS.map(periodLabel)).toEqual([
      'Last 7 days',
      'Last 30 days',
      'Last 90 days',
      'This month',
      'Last month',
    ]);
  });

  it('starts on the last 30 days', () => {
    expect(DEFAULT_USAGE_PERIOD).toBe('last-30-days');
  });
});

describe('isUsagePeriod', () => {
  it('knows the periods and nothing else', () => {
    expect(USAGE_PERIODS.every(isUsagePeriod)).toBe(true);
    expect(isUsagePeriod('')).toBe(false);
    expect(isUsagePeriod('current')).toBe(false);
  });
});

describe('dayLabel', () => {
  it('reads as the month and the day', () => {
    expect(dayLabel(day(2026, 10, 9))).toBe('Oct 9');
    expect(dayLabel(day(2026, 1, 31))).toBe('Jan 31');
  });
});

describe('rangeLabel', () => {
  const range = { fromDay: day(2026, 9, 10), toDay: day(2026, 10, 9) };

  it('says the days and the zone they were counted in', () => {
    expect(rangeLabel(range, 'Europe/Paris')).toBe(
      'Sep 10 – Oct 9, 2026 · days in Europe/Paris'
    );
    expect(rangeLabel(range, 'UTC')).toBe('Sep 10 – Oct 9, 2026 · days in UTC');
  });

  it('says the days alone while the zone is not known', () => {
    expect(rangeLabel(range, undefined)).toBe('Sep 10 – Oct 9, 2026');
    expect(rangeLabel(range, '')).toBe('Sep 10 – Oct 9, 2026');
  });

  it('says both years when the days are of two', () => {
    expect(
      rangeLabel({ fromDay: day(2026, 12, 20), toDay: day(2027, 1, 5) }, 'UTC')
    ).toBe('Dec 20, 2026 – Jan 5, 2027 · days in UTC');
  });

  it('says one day once', () => {
    expect(
      rangeLabel({ fromDay: day(2026, 10, 9), toDay: day(2026, 10, 9) }, 'UTC')
    ).toBe('Oct 9, 2026 · days in UTC');
  });
});
