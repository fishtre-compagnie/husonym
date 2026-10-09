import type { UsageDay } from '@husonym/sdk';
import { dayLabel } from './period';

// A day as the chart plots it and as its table reads it.
interface DayPoint {
  key: string;
  label: string;
  rowsRead: number;
  runs: number;
}

// The days of an answer, ready to plot. The API gives every day of the period, the
// oldest first, a day without a run at zero: nothing is filled or sorted here. A day
// that tells no date cannot be placed and is left out.
export function dayPoints(
  days: readonly Pick<UsageDay, 'day' | 'rowsRead' | 'runs'>[]
): DayPoint[] {
  return days.flatMap(({ day, rowsRead, runs }) =>
    day
      ? [
          {
            key: `${day.year}-${day.month}-${day.day}`,
            label: dayLabel(day),
            rowsRead: Number(rowsRead),
            runs: Number(runs),
          },
        ]
      : []
  );
}
