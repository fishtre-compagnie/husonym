'use client';

import { Panel, PanelHeader } from '@/components/ds';
import { dayPoints } from '@/libs/usage/days';
import { countFormat, formatCount } from '@/libs/usage/numbers';
import { useUsageI18n } from '@/libs/usage/useUsageI18n';
import type { UsageI18n } from '@/libs/usage/i18n';
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

// The colors are the variables of the theme, so that the chart follows it without
// being told which one is on.
const TICK = { fill: 'var(--muted-foreground)', fontSize: 12 };

// The rows read on each day of a period: one bar per day, a day without a run at zero.
// The drawing is for the eye only; the same numbers are in a table for a screen reader.
export default function RowsPerDayChart(props: Props): ReactElement {
  const { days, emptyLine } = props;
  const i18n = useUsageI18n();
  const { t, locale } = i18n;
  const points = dayPoints(days, locale);
  // Made here, not once for the module: the module is also loaded where there is no
  // browser to tell its languages.
  const axisNumbers = countFormat({ maximumFractionDigits: 1 }, locale);
  return (
    <Panel>
      <PanelHeader title={t('chart.title')} />
      <div className="p-5">
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
                      shortNumberFormatter(axisNumbers, value)
                    }
                  />
                  <Tooltip
                    cursor={{ fill: 'var(--hover)' }}
                    content={(tooltip: TooltipContentProps) =>
                      dayTooltip(tooltip, i18n)
                    }
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
              <caption>{t('chart.title')}</caption>
              <thead>
                <tr>
                  <th scope="col">{t('chart.day')}</th>
                  <th scope="col">{t('chart.rowsRead')}</th>
                  <th scope="col">{t('chart.runs')}</th>
                </tr>
              </thead>
              <tbody>
                {points.map((point) => (
                  <tr key={point.key}>
                    <th scope="row">{point.label}</th>
                    <td>{formatCount(point.rowsRead, locale)}</td>
                    <td>{formatCount(point.runs, locale)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </div>
    </Panel>
  );
}

// What the pointer shows over a day: the day, its rows and its runs.
function dayTooltip(
  { active, payload }: TooltipContentProps,
  { t, locale }: UsageI18n
): ReactNode {
  const point: DayPoint | undefined = active
    ? payload?.[0]?.payload
    : undefined;
  if (!point) {
    return null;
  }
  return (
    <div className="rounded-lg border border-border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-pop">
      <div className="font-medium">{point.label}</div>
      <div className="tabular-nums">
        {t('chart.tooltipRowsRead', {
          count: formatCount(point.rowsRead, locale),
        })}
      </div>
      <div className="tabular-nums">
        {t('chart.tooltipRuns', { count: formatCount(point.runs, locale) })}
      </div>
    </div>
  );
}
