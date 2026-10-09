'use client';

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { dayPoints } from '@/libs/usage/days';
import { formatCount } from '@/libs/usage/totals';
import { UsageDay } from '@husonym/sdk';
import { ReactElement, ReactNode } from 'react';
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  TooltipContentProps,
  XAxis,
  YAxis,
} from 'recharts';
import { shortNumberFormatter } from './util';

interface Props {
  // Every day of the period, the oldest first, as the API gives them.
  days: readonly UsageDay[];
  // What to say in place of the chart, when the period has nothing to plot.
  emptyLine?: string;
}

type DayPoint = ReturnType<typeof dayPoints>[number];

const AXIS_NUMBERS = new Intl.NumberFormat('en-US', {
  maximumFractionDigits: 1,
});

// The colors are the variables of the theme, so that the chart follows it without
// being told which one is on.
const TICK = { fill: 'var(--muted-foreground)', fontSize: 12 };

// The rows read on each day of a period: one bar per day, a day without a run at zero.
// The drawing is for the eye only; the same numbers are in a table for a screen reader.
export default function RowsPerDayChart(props: Props): ReactElement {
  const { days, emptyLine } = props;
  const points = dayPoints(days);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Rows read per day</CardTitle>
      </CardHeader>
      <CardContent>
        {emptyLine ? (
          <p className="text-sm text-muted-foreground">{emptyLine}</p>
        ) : (
          <>
            <div aria-hidden="true">
              <ResponsiveContainer width="100%" height={320}>
                <BarChart
                  data={points}
                  accessibilityLayer={false}
                  margin={{ top: 8, right: 8, bottom: 0, left: 0 }}
                >
                  <CartesianGrid
                    vertical={false}
                    strokeDasharray="3 3"
                    stroke="var(--border)"
                  />
                  {/* The axis drops the labels that would run into each other. */}
                  <XAxis
                    dataKey="label"
                    tick={TICK}
                    tickLine={false}
                    axisLine={{ stroke: 'var(--border)' }}
                    interval="preserveStartEnd"
                    minTickGap={24}
                  />
                  <YAxis
                    allowDecimals={false}
                    tick={TICK}
                    tickLine={false}
                    axisLine={false}
                    width={56}
                    tickFormatter={(value: number) =>
                      shortNumberFormatter(AXIS_NUMBERS, value)
                    }
                  />
                  <Tooltip
                    cursor={{ fill: 'var(--muted)' }}
                    content={DayTooltip}
                  />
                  <Bar
                    dataKey="rowsRead"
                    fill="var(--primary)"
                    radius={[2, 2, 0, 0]}
                    maxBarSize={48}
                    isAnimationActive={false}
                  />
                </BarChart>
              </ResponsiveContainer>
            </div>
            <table className="sr-only">
              <caption>Rows read per day</caption>
              <thead>
                <tr>
                  <th scope="col">Day</th>
                  <th scope="col">Rows read</th>
                  <th scope="col">Runs</th>
                </tr>
              </thead>
              <tbody>
                {points.map((point) => (
                  <tr key={point.key}>
                    <th scope="row">{point.label}</th>
                    <td>{formatCount(point.rowsRead)}</td>
                    <td>{formatCount(point.runs)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </CardContent>
    </Card>
  );
}

// What the pointer shows over a day: the day, its rows and its runs.
function DayTooltip({ active, payload }: TooltipContentProps): ReactNode {
  const point: DayPoint | undefined = active
    ? payload?.[0]?.payload
    : undefined;
  if (!point) {
    return null;
  }
  return (
    <div className="rounded-md border border-gray-200 dark:border-gray-700 bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
      <div className="font-medium">{point.label}</div>
      <div className="tabular-nums">
        Rows read: {formatCount(point.rowsRead)}
      </div>
      <div className="tabular-nums">Runs: {formatCount(point.runs)}</div>
    </div>
  );
}
