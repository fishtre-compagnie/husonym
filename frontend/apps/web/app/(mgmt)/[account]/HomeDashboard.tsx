'use client';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/libs/utils';
import { useQuery } from '@connectrpc/connect-query';
import {
  ConnectionService,
  JobEngine,
  JobRunStatus,
  JobService,
} from '@husonym/sdk';
import { useTheme } from 'next-themes';
import { ReactElement, useMemo, useState } from 'react';
import { Cell, Pie, PieChart, ResponsiveContainer, Sector } from 'recharts';

interface Props {
  accountId: string;
}

function usePalette() {
  const { resolvedTheme } = useTheme();
  const dark = resolvedTheme === 'dark';
  return {
    surface: dark ? '#1a1a19' : '#fcfcfb',
    good: '#0ca30c',
    warning: '#fab219',
    critical: '#d03b3b',
    muted: '#898781',
    blue: dark ? '#3987e5' : '#2a78d6',
    orange: dark ? '#d95926' : '#eb6834',
  };
}

interface Slice {
  name: string;
  value: number;
  color: string;
}

export default function HomeDashboard({ accountId }: Props): ReactElement {
  const pal = usePalette();

  const { data: jobsData, isLoading: jobsLoading } = useQuery(
    JobService.method.getJobs,
    { accountId },
    { enabled: !!accountId }
  );
  const { data: runsData, isLoading: runsLoading } = useQuery(
    JobService.method.getJobRuns,
    { id: { case: 'accountId', value: accountId } },
    { enabled: !!accountId }
  );
  const { data: connsData, isLoading: connsLoading } = useQuery(
    ConnectionService.method.getConnections,
    { accountId },
    { enabled: !!accountId }
  );

  const isLoading = jobsLoading || runsLoading || connsLoading;

  const { runSlices, engineSlices, totals } = useMemo(() => {
    const jobs = jobsData?.jobs ?? [];
    const runs = runsData?.jobRuns ?? [];
    const conns = connsData?.connections ?? [];

    const runBuckets: Record<string, { value: number; color: string }> = {
      Completed: { value: 0, color: pal.good },
      Running: { value: 0, color: pal.blue },
      Pending: { value: 0, color: pal.warning },
      Failed: { value: 0, color: pal.critical },
      Canceled: { value: 0, color: pal.muted },
    };
    for (const r of runs) {
      switch (r.status) {
        case JobRunStatus.COMPLETE:
          runBuckets.Completed.value++;
          break;
        case JobRunStatus.RUNNING:
          runBuckets.Running.value++;
          break;
        case JobRunStatus.PENDING:
          runBuckets.Pending.value++;
          break;
        case JobRunStatus.ERROR:
        case JobRunStatus.FAILED:
        case JobRunStatus.TIMED_OUT:
          runBuckets.Failed.value++;
          break;
        case JobRunStatus.CANCELED:
        case JobRunStatus.TERMINATED:
          runBuckets.Canceled.value++;
          break;
      }
    }
    const runSlices: Slice[] = Object.entries(runBuckets)
      .map(([name, b]) => ({ name, value: b.value, color: b.color }))
      .filter((s) => s.value > 0);

    const engineBuckets: Record<string, { value: number; color: string }> = {
      Athanor: { value: 0, color: pal.blue },
      Benthos: { value: 0, color: pal.orange },
      Default: { value: 0, color: pal.muted },
    };
    for (const j of jobs) {
      switch (j.workflowOptions?.engine) {
        case JobEngine.ATHANOR:
          engineBuckets.Athanor.value++;
          break;
        case JobEngine.BENTHOS:
          engineBuckets.Benthos.value++;
          break;
        default:
          engineBuckets.Default.value++;
          break;
      }
    }
    const engineSlices: Slice[] = Object.entries(engineBuckets)
      .map(([name, b]) => ({ name, value: b.value, color: b.color }))
      .filter((s) => s.value > 0);

    const completed = runBuckets.Completed.value;
    const successRate =
      runs.length > 0 ? Math.round((completed / runs.length) * 100) : null;

    return {
      runSlices,
      engineSlices,
      totals: {
        jobs: jobs.length,
        connections: conns.length,
        runs: runs.length,
        successRate,
      },
    };
  }, [jobsData, runsData, connsData, pal]);

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-24 w-full" />
          ))}
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <Skeleton className="h-80 w-full" />
          <Skeleton className="h-80 w-full" />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatTile label="Jobs" value={totals.jobs} />
        <StatTile label="Connections" value={totals.connections} />
        <StatTile label="Job runs" value={totals.runs} />
        <StatTile
          label="Success rate"
          value={totals.successRate === null ? '—' : `${totals.successRate}%`}
          accent={
            totals.successRate === null
              ? undefined
              : totals.successRate >= 90
                ? pal.good
                : totals.successRate >= 50
                  ? pal.warning
                  : pal.critical
          }
        />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <DonutCard
          title="Job runs by outcome"
          slices={runSlices}
          total={totals.runs}
          totalLabel="runs"
          surface={pal.surface}
          emptyText="No job runs yet"
        />
        <DonutCard
          title="Jobs by engine"
          slices={engineSlices}
          total={totals.jobs}
          totalLabel="jobs"
          surface={pal.surface}
          emptyText="No jobs yet"
        />
      </div>
    </div>
  );
}

function StatTile({
  label,
  value,
  accent,
}: {
  label: string;
  value: number | string;
  accent?: string;
}): ReactElement {
  return (
    <Card className="transition-shadow hover:shadow-md">
      <CardContent className="flex flex-col gap-1 pt-6">
        <span
          className="text-3xl font-semibold tabular-nums"
          style={accent ? { color: accent } : undefined}
        >
          {value}
        </span>
        <span className="text-sm text-muted-foreground">{label}</span>
      </CardContent>
    </Card>
  );
}

// Secteur agrandi au survol (effet « zoom » + anneau externe qui « pop »).
function renderActiveSector(props: {
  cx?: number;
  cy?: number;
  innerRadius?: number;
  outerRadius?: number;
  startAngle?: number;
  endAngle?: number;
  fill?: string;
}) {
  const {
    cx = 0,
    cy = 0,
    innerRadius = 0,
    outerRadius = 0,
    startAngle = 0,
    endAngle = 0,
    fill,
  } = props;
  return (
    <g>
      <Sector
        cx={cx}
        cy={cy}
        innerRadius={innerRadius}
        outerRadius={outerRadius + 6}
        startAngle={startAngle}
        endAngle={endAngle}
        fill={fill}
      />
      <Sector
        cx={cx}
        cy={cy}
        innerRadius={outerRadius + 9}
        outerRadius={outerRadius + 12}
        startAngle={startAngle}
        endAngle={endAngle}
        fill={fill}
      />
    </g>
  );
}

interface DonutCardProps {
  title: string;
  slices: Slice[];
  total: number;
  totalLabel: string;
  surface: string;
  emptyText: string;
}

function DonutCard({
  title,
  slices,
  total,
  totalLabel,
  surface,
  emptyText,
}: DonutCardProps): ReactElement {
  const [active, setActive] = useState<number | null>(null);
  const activeSlice = active !== null ? slices[active] : null;
  const pct = (v: number) => (total > 0 ? Math.round((v / total) * 100) : 0);

  return (
    <Card className="transition-shadow hover:shadow-md">
      <CardHeader>
        <CardTitle className="text-base">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {slices.length === 0 ? (
          <div className="flex h-60 items-center justify-center text-sm text-muted-foreground">
            {emptyText}
          </div>
        ) : (
          <div className="flex flex-col sm:flex-row items-center gap-4">
            <div className="relative h-60 w-full sm:w-1/2">
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={slices}
                    dataKey="value"
                    nameKey="name"
                    innerRadius={60}
                    outerRadius={86}
                    paddingAngle={2}
                    stroke={surface}
                    strokeWidth={2}
                    activeShape={renderActiveSector}
                    onMouseEnter={(_, i) => setActive(i)}
                    onMouseLeave={() => setActive(null)}
                    animationDuration={500}
                  >
                    {slices.map((s, i) => (
                      <Cell
                        key={s.name}
                        fill={s.color}
                        fillOpacity={active === null || active === i ? 1 : 0.35}
                        style={{ transition: 'fill-opacity 150ms' }}
                      />
                    ))}
                  </Pie>
                </PieChart>
              </ResponsiveContainer>
              {/* Label central dynamique : total, ou détail du segment survolé. */}
              <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
                {activeSlice ? (
                  <>
                    <span
                      className="text-2xl font-semibold tabular-nums"
                      style={{ color: activeSlice.color }}
                    >
                      {activeSlice.value}
                    </span>
                    <span className="text-xs font-medium">
                      {activeSlice.name}
                    </span>
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {pct(activeSlice.value)}%
                    </span>
                  </>
                ) : (
                  <>
                    <span className="text-2xl font-semibold tabular-nums">
                      {total}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {totalLabel}
                    </span>
                  </>
                )}
              </div>
            </div>

            {/* Légende interactive, synchronisée avec le survol du donut. */}
            <ul className="flex w-full flex-col gap-1 sm:w-1/2">
              {slices.map((s, i) => (
                <li key={s.name}>
                  <button
                    type="button"
                    onMouseEnter={() => setActive(i)}
                    onMouseLeave={() => setActive(null)}
                    className={cn(
                      'flex w-full items-center justify-between rounded-md px-2 py-1.5 text-sm transition-colors',
                      active === i ? 'bg-muted' : 'hover:bg-muted/60',
                      active !== null && active !== i && 'opacity-50'
                    )}
                  >
                    <span className="flex items-center gap-2">
                      <span
                        className="inline-block h-3 w-3 rounded-sm"
                        style={{ backgroundColor: s.color }}
                      />
                      <span
                        className={cn(active === i && 'font-medium')}
                      >
                        {s.name}
                      </span>
                    </span>
                    <span className="tabular-nums text-muted-foreground">
                      {s.value}
                      <span className="ml-1 text-xs">({pct(s.value)}%)</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
