'use client';
/**
 * The charts of the home dashboard. Their colors are the --viz-* tokens of globals.css,
 * chosen to be told apart with a color vision deficiency: blue for what succeeded, orange
 * for what failed, gray for what was canceled. A color never speaks alone: a legend, a
 * label or a tooltip says the same.
 */
import { Panel } from '@/components/ds';
import { cn } from '@/libs/utils';
import { Cross2Icon } from '@radix-ui/react-icons';
import { useFormatter, useTranslations } from 'next-intl';
import Link from 'next/link';
import { ReactElement, ReactNode, useMemo, useState } from 'react';
import { formatSpan } from './dashboard-data';
import {
  DayOutcomes,
  HeatSlot,
  heatLevel,
  niceMax,
} from './dashboard-analytics';

const RAMP = [
  'bg-[var(--viz-heat-0)]',
  'bg-[var(--viz-heat-1)]',
  'bg-[var(--viz-heat-2)]',
  'bg-[var(--viz-heat-3)]',
  'bg-[var(--viz-heat-4)]',
  'bg-[var(--viz-heat-5)]',
];

/** A key figure: its value, how it moved since the period before, and a small chart. */
export function KpiTile(props: {
  label: string;
  value: ReactNode;
  delta?: { text: string; up: boolean };
  note: ReactNode;
  chart?: ReactNode;
}): ReactElement {
  const { label, value, delta, note, chart } = props;
  return (
    <Panel className="flex min-h-[148px] flex-col gap-2.5 px-[18px] pb-3.5 pt-4">
      <span className="text-[12.5px] font-medium text-muted-foreground">
        {label}
      </span>
      <div className="flex items-end justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <span className="font-display text-[32px] font-semibold leading-none tracking-tight tabular-nums">
            {value}
          </span>
          {delta && (
            <span className="inline-flex items-center gap-1 text-xs font-medium text-quiet-fg">
              <svg
                width="11"
                height="11"
                viewBox="0 0 12 12"
                aria-hidden="true"
              >
                <path
                  d={delta.up ? 'M3 8l3-3 3 3' : 'M3 4l3 3 3-3'}
                  fill="none"
                  stroke="currentColor"
                  strokeWidth={1.8}
                  strokeLinecap="round"
                  strokeLinejoin="round"
                />
              </svg>
              {delta.text}
            </span>
          )}
        </div>
        {chart}
      </div>
      <span className="text-[12.5px] text-muted-foreground">{note}</span>
    </Panel>
  );
}

/** A line with the area under it, scaled to its highest point. Decorative. */
export function AreaSpark(props: { values: number[] }): ReactElement {
  const { values } = props;
  const w = 120;
  const h = 44;
  const max = Math.max(1, ...values);
  const step = w / Math.max(values.length - 1, 1);
  const pts = values.map(
    (v, i) =>
      `${(i * step).toFixed(1)},${(h - 3 - (v / max) * (h - 8)).toFixed(1)}`
  );
  return (
    <svg
      width={w}
      height={h}
      viewBox={`0 0 ${w} ${h}`}
      className="shrink-0 text-[var(--viz-ok)]"
      aria-hidden="true"
    >
      <path
        d={`M0,${h} L${pts.join(' L')} L${w},${h} Z`}
        fill="currentColor"
        fillOpacity={0.12}
      />
      <polyline
        points={pts.join(' ')}
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

/** A ring filled to `percent`. Decorative: the figure beside it says the value. */
export function Ring(props: { percent: number }): ReactElement {
  const r = 26;
  const c = 2 * Math.PI * r;
  return (
    <svg
      width="64"
      height="64"
      viewBox="0 0 64 64"
      className="shrink-0"
      aria-hidden="true"
    >
      <circle
        cx="32"
        cy="32"
        r={r}
        fill="none"
        className="stroke-muted"
        strokeWidth={7}
      />
      <circle
        cx="32"
        cy="32"
        r={r}
        fill="none"
        className="stroke-[var(--viz-ok)]"
        strokeWidth={7}
        strokeLinecap="round"
        strokeDasharray={`${((c * Math.max(0, Math.min(100, props.percent))) / 100).toFixed(1)} ${c.toFixed(1)}`}
        transform="rotate(-90 32 32)"
      />
    </svg>
  );
}

type Measure = 'runs' | 'rows';

/**
 * The runs of each day by outcome, or the rows the runs read, as bars. Hovering or
 * focusing a day tells it in full.
 */
export function ActivityChart(props: {
  outcomes: DayOutcomes[];
  // The rows read on each day, aligned with `outcomes`.
  rows: number[];
}): ReactElement {
  const { outcomes, rows } = props;
  const t = useTranslations('dashboard.home.activity');
  const format = useFormatter();
  const [measure, setMeasure] = useState<Measure>('runs');
  const [hover, setHover] = useState<number | null>(null);

  const isRuns = measure === 'runs';
  const values = outcomes.map((d, i) =>
    isRuns ? d.succeeded + d.failed + d.canceled : (rows[i] ?? 0)
  );
  const top = niceMax(Math.max(0, ...values));
  const n = outcomes.length;
  const tickEvery = Math.max(1, Math.round(n / 6));
  const H = 220;
  const fmt = (v: number): string =>
    isRuns ? format.number(v) : format.number(v, { notation: 'compact' });
  const h = hover !== null ? outcomes[hover] : undefined;
  const left = hover !== null ? ((hover + 0.5) / n) * 100 : 0;

  return (
    <Panel className="flex min-w-0 flex-[999_1_640px] flex-col gap-3.5 px-5 pb-4 pt-[18px]">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <h2 className="m-0 text-[15px] font-semibold">{t('title')}</h2>
          <span className="text-[12.5px] text-muted-foreground">
            {isRuns ? t('subRuns') : t('subRows')}
          </span>
        </div>
        <div
          role="group"
          aria-label={t('measure')}
          className="flex gap-0.5 rounded-[9px] bg-surface-2 p-[3px]"
        >
          {(['runs', 'rows'] as Measure[]).map((m) => (
            <button
              key={m}
              type="button"
              aria-pressed={measure === m}
              onClick={() => setMeasure(m)}
              className={cn(
                'cursor-pointer rounded-[7px] px-3 py-1.5 text-[12.5px] font-medium transition-colors',
                measure === m
                  ? 'bg-card text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              )}
            >
              {m === 'runs' ? t('runs') : t('rows')}
            </button>
          ))}
        </div>
      </div>

      {isRuns && (
        <div className="flex flex-wrap gap-4 text-[12.5px] text-muted-foreground">
          <Swatch className="bg-[var(--viz-ok)]">{t('succeeded')}</Swatch>
          <Swatch className="bg-[var(--viz-fail)]">{t('failed')}</Swatch>
          <Swatch className="bg-[var(--viz-cancel)]">{t('canceled')}</Swatch>
        </div>
      )}

      <div className="flex gap-2">
        <div
          className="flex w-[38px] shrink-0 flex-col justify-between text-right text-[11px] text-muted-foreground tabular-nums"
          style={{ height: H }}
          aria-hidden="true"
        >
          <span className="-translate-y-1.5">{fmt(top)}</span>
          <span>{fmt(top / 2)}</span>
          <span className="translate-y-1.5">0</span>
        </div>
        <div
          className="relative min-w-0 grow"
          onMouseLeave={() => setHover(null)}
        >
          <div
            className="pointer-events-none absolute inset-x-0 top-0 flex flex-col justify-between"
            style={{ height: H }}
            aria-hidden="true"
          >
            <div className="border-t border-dashed border-line-soft" />
            <div className="border-t border-dashed border-line-soft" />
            <div className="border-t border-border" />
          </div>
          <div
            className={cn(
              'relative flex items-end',
              n > 60 ? 'gap-px' : n > 14 ? 'gap-[3px]' : 'gap-2.5'
            )}
            style={{ height: H }}
          >
            {outcomes.map((d, i) => {
              const total = d.succeeded + d.failed + d.canceled;
              const label = t('dayAria', {
                day: format.dateTime(d.day, {
                  weekday: 'long',
                  day: 'numeric',
                  month: 'long',
                }),
                runs: total,
                rows: rows[i] ?? 0,
              });
              const seg = (v: number): number =>
                v > 0 ? Math.max((v / top) * H - 2, 2) : 0;
              return (
                <button
                  key={d.day.getTime()}
                  type="button"
                  aria-label={label}
                  onMouseEnter={() => setHover(i)}
                  onFocus={() => setHover(i)}
                  onBlur={() => setHover(null)}
                  className="group relative flex h-full min-w-0 flex-1 cursor-default justify-center rounded-md focus-visible:outline-2 focus-visible:outline-link"
                >
                  <span className="absolute -inset-x-px inset-y-0 rounded-md group-hover:bg-hover" />
                  <span
                    className={cn(
                      'relative flex h-full max-w-full flex-col justify-end self-end transition-opacity',
                      n > 60 ? 'w-1.5' : n > 14 ? 'w-3.5' : 'w-[30px]',
                      hover !== null && hover !== i && 'opacity-55'
                    )}
                  >
                    {isRuns ? (
                      <StackedBar
                        parts={[
                          { h: seg(d.canceled), c: 'bg-[var(--viz-cancel)]' },
                          { h: seg(d.failed), c: 'bg-[var(--viz-fail)]' },
                          { h: seg(d.succeeded), c: 'bg-[var(--viz-ok)]' },
                        ]}
                      />
                    ) : (
                      <StackedBar
                        parts={[
                          { h: seg(rows[i] ?? 0), c: 'bg-[var(--viz-ok)]' },
                        ]}
                      />
                    )}
                  </span>
                </button>
              );
            })}
          </div>
          <div className="mt-2 flex justify-between text-[11px] text-muted-foreground">
            {outcomes
              .filter((_, i) => i % tickEvery === 0 || i === n - 1)
              .map((d) => (
                <span key={d.day.getTime()}>
                  {format.dateTime(d.day, { day: 'numeric', month: 'short' })}
                </span>
              ))}
          </div>

          {h && hover !== null && (
            <div
              role="status"
              className="pointer-events-none absolute top-2 z-10 flex flex-col gap-1.5 whitespace-nowrap rounded-[10px] bg-foreground px-3 py-2.5 text-xs text-background shadow-pop"
              style={{
                left: `${left}%`,
                transform:
                  left > 70
                    ? 'translateX(calc(-100% - 12px))'
                    : 'translateX(12px)',
              }}
            >
              <span className="text-[12.5px] font-semibold">
                {format.dateTime(h.day, {
                  weekday: 'long',
                  day: 'numeric',
                  month: 'long',
                })}
              </span>
              <TipRow className="bg-[var(--viz-ok)]">
                {t('tipSucceeded', { count: h.succeeded })}
              </TipRow>
              <TipRow className="bg-[var(--viz-fail)]">
                {t('tipFailed', { count: h.failed })}
              </TipRow>
              <TipRow className="bg-[var(--viz-cancel)]">
                {t('tipCanceled', { count: h.canceled })}
              </TipRow>
              <span className="opacity-75">
                {t('tipRows', { rows: format.number(rows[hover] ?? 0) })}
              </span>
            </div>
          )}
        </div>
      </div>
    </Panel>
  );
}

// The parts of a bar from top to bottom; the top one rounds its ends, 2 px apart.
function StackedBar(props: {
  parts: { h: number; c: string }[];
}): ReactElement {
  const shown = props.parts.filter((p) => p.h > 0);
  return (
    <>
      {shown.map((p, i) => (
        <span
          key={i}
          className={cn(
            'block w-full',
            p.c,
            i === 0 && 'rounded-t',
            i > 0 && 'mt-0.5'
          )}
          style={{ height: p.h }}
        />
      ))}
    </>
  );
}

function Swatch(props: {
  className: string;
  children: ReactNode;
}): ReactElement {
  return (
    <span className="flex items-center gap-1.5">
      <span
        className={cn('size-2.5 rounded-[3px]', props.className)}
        aria-hidden="true"
      />
      {props.children}
    </span>
  );
}

function TipRow(props: {
  className: string;
  children: ReactNode;
}): ReactElement {
  return (
    <span className="flex items-center gap-1.5">
      <span
        className={cn('size-2 rounded-[2px]', props.className)}
        aria-hidden="true"
      />
      {props.children}
    </span>
  );
}

/**
 * When the jobs run: a cell per hour of each day of the week, darker the more runs started
 * in it. Hovering or focusing a cell tells which jobs ran then; clicking pins that card.
 */
export function RunHeatmap(props: {
  slots: HeatSlot[][];
  days: number;
  jobName(jobId: string): string;
  runsHref: string;
}): ReactElement {
  const { slots, days, jobName, runsHref } = props;
  const t = useTranslations('dashboard.home.heatmap');
  const format = useFormatter();
  const [active, setActive] = useState<{ d: number; h: number } | null>(null);
  const [pinned, setPinned] = useState(false);

  const busiest = useMemo(
    () => Math.max(0, ...slots.flat().map((s) => s.runs)),
    [slots]
  );
  // Monday 2026-01-05 and the days after it, to name the days of the week in the locale.
  const dayName = (d: number, style: 'short' | 'long'): string =>
    format.dateTime(new Date(2026, 0, 5 + d), { weekday: style });
  const hourLabel = (h: number): string => t('hour', { hour: h });

  const slot = active ? slots[active.d][active.h] : undefined;
  const right = active ? active.h >= 14 : false;
  const below = active ? active.d < 4 : true;

  return (
    <Panel className="flex flex-col gap-3.5 px-5 py-[18px]">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <h2 className="m-0 text-[15px] font-semibold">{t('title')}</h2>
          <span className="text-[12.5px] text-muted-foreground">
            {t('sub')}
          </span>
        </div>
        <div
          className="flex items-center gap-1.5 text-[11.5px] text-muted-foreground"
          aria-hidden="true"
        >
          <span>{t('less')}</span>
          {RAMP.map((c) => (
            <span key={c} className={cn('h-3 w-4 rounded-[3px]', c)} />
          ))}
          <span>{t('more')}</span>
        </div>
      </div>

      {busiest === 0 ? (
        <p className="m-0 py-6 text-center text-sm text-muted-foreground">
          {t('empty', { days })}
        </p>
      ) : (
        <div
          className="relative flex flex-col gap-[3px]"
          onMouseLeave={() => {
            if (!pinned) setActive(null);
          }}
        >
          {slots.map((row, d) => (
            <div key={d} className="flex items-center gap-[3px]">
              <span className="w-[34px] shrink-0 text-[11.5px] capitalize text-muted-foreground">
                {dayName(d, 'short')}
              </span>
              {row.map((s, h) => {
                const isActive = active?.d === d && active?.h === h;
                const label = t('cell', {
                  day: dayName(d, 'long'),
                  hour: hourLabel(h),
                  runs: s.runs,
                });
                return (
                  <button
                    key={h}
                    type="button"
                    aria-label={label}
                    aria-expanded={s.runs > 0 ? isActive : undefined}
                    onMouseEnter={() => {
                      if (!pinned) setActive(s.runs > 0 ? { d, h } : null);
                    }}
                    onFocus={() => {
                      if (!pinned) setActive(s.runs > 0 ? { d, h } : null);
                    }}
                    onClick={() => {
                      if (s.runs > 0) {
                        setActive({ d, h });
                        setPinned(true);
                      }
                    }}
                    className={cn(
                      'h-[22px] min-w-0 flex-1 rounded-[4px] focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-foreground',
                      RAMP[heatLevel(s.runs, busiest)],
                      s.runs > 0 ? 'cursor-pointer' : 'cursor-default',
                      isActive &&
                        'shadow-[0_0_0_2px_var(--card),0_0_0_4px_var(--foreground)]'
                    )}
                  />
                );
              })}
            </div>
          ))}
          <div
            className="flex gap-[3px] pl-[37px] text-[11px] text-muted-foreground"
            aria-hidden="true"
          >
            {Array.from({ length: 24 }, (_, h) => (
              <span key={h} className="flex-1 text-center">
                {h % 3 === 0 ? hourLabel(h) : ''}
              </span>
            ))}
          </div>

          {slot && active && (
            <div
              role="dialog"
              aria-label={t('popTitle', {
                day: dayName(active.d, 'long'),
                from: hourLabel(active.h),
                to: hourLabel((active.h + 1) % 24),
              })}
              className={cn(
                'absolute z-20 flex w-[300px] flex-col gap-2.5 rounded-xl border border-line-soft bg-card p-3 shadow-pop',
                pinned ? 'pointer-events-auto' : 'pointer-events-none'
              )}
              style={{
                ...(right
                  ? {
                      right: `calc((100% - 37px) * ${((24 - active.h) / 24).toFixed(4)} + 10px)`,
                    }
                  : {
                      left: `calc(37px + (100% - 37px) * ${((active.h + 1) / 24).toFixed(4)} + 10px)`,
                    }),
                ...(below
                  ? { top: active.d * 25 }
                  : { bottom: (6 - active.d) * 25 + 20 }),
              }}
            >
              <div className="flex items-start justify-between gap-3">
                <div className="flex flex-col gap-0.5">
                  <span className="text-[13.5px] font-semibold capitalize">
                    {t('popTitle', {
                      day: dayName(active.d, 'long'),
                      from: hourLabel(active.h),
                      to: hourLabel((active.h + 1) % 24),
                    })}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {t('popSub', {
                      runs: slot.runs,
                      days,
                      failed: slot.failed,
                    })}
                  </span>
                </div>
                {pinned && (
                  <button
                    type="button"
                    aria-label={t('close')}
                    onClick={() => {
                      setPinned(false);
                      setActive(null);
                    }}
                    className="grid size-[26px] shrink-0 cursor-pointer place-items-center rounded-[7px] bg-surface-2 text-muted-foreground hover:text-foreground"
                  >
                    <Cross2Icon className="size-3" />
                  </button>
                )}
              </div>
              <div
                className="flex h-1.5 gap-0.5 overflow-hidden rounded-full"
                aria-hidden="true"
              >
                <span
                  className="block h-full rounded-full bg-[var(--viz-ok)]"
                  style={{
                    width: `${((slot.runs - slot.failed) / slot.runs) * 100}%`,
                  }}
                />
                {slot.failed > 0 && (
                  <span className="block h-full grow rounded-full bg-[var(--viz-fail)]" />
                )}
              </div>
              <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
                {slot.jobs.slice(0, 5).map((j) => (
                  <li
                    key={j.jobId}
                    className="flex items-center gap-2.5 rounded-lg bg-surface-2/60 px-2 py-[7px]"
                  >
                    <span
                      className={cn(
                        'size-2 shrink-0 rounded-full',
                        j.failed > 0
                          ? 'bg-[var(--viz-fail)]'
                          : 'bg-[var(--viz-ok)]'
                      )}
                      aria-hidden="true"
                    />
                    <span className="flex min-w-0 grow flex-col gap-px">
                      <span className="truncate text-[13px] font-medium">
                        {jobName(j.jobId)}
                      </span>
                      <span className="text-[11.5px] text-muted-foreground">
                        {[
                          j.failed > 0
                            ? t('jobFailed', { count: j.failed })
                            : t('jobOk'),
                          j.meanMs !== undefined
                            ? t('jobMean', { duration: formatSpan(j.meanMs) })
                            : null,
                        ]
                          .filter(Boolean)
                          .join(' · ')}
                      </span>
                    </span>
                    <span className="rounded-full bg-brand-soft px-2 py-0.5 text-xs font-semibold text-primary tabular-nums">
                      ×{format.number(j.runs)}
                    </span>
                  </li>
                ))}
              </ul>
              {slot.jobs.length > 5 && (
                <span className="text-xs text-muted-foreground">
                  {t('otherJobs', { count: slot.jobs.length - 5 })}
                </span>
              )}
              {pinned && (
                <Link
                  href={runsHref}
                  className="self-end text-[12.5px] font-medium text-link hover:underline"
                >
                  {t('seeRuns')}
                </Link>
              )}
            </div>
          )}
        </div>
      )}
    </Panel>
  );
}

/** A job of the period, as the usage counters and its last run tell it. */
export interface JobRow {
  jobId: string;
  name: string;
  kind: string;
  runs: number;
  rate?: number;
  rows: number;
  medianSeconds?: number;
  last?: { outcome: 'succeeded' | 'failed' | 'canceled'; label: string };
  href: string;
}

type SortKey = 'runs' | 'rate' | 'rows' | 'median';

/** The jobs that ran in the period, sortable by each figure, the most rows read first. */
export function JobsTable(props: { rows: JobRow[] }): ReactElement {
  const t = useTranslations('dashboard.home.jobsTable');
  const format = useFormatter();
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({
    key: 'rows',
    dir: -1,
  });
  const value = (r: JobRow, key: SortKey): number =>
    key === 'runs'
      ? r.runs
      : key === 'rate'
        ? (r.rate ?? -1)
        : key === 'rows'
          ? r.rows
          : (r.medianSeconds ?? -1);
  const rows = [...props.rows].sort(
    (a, b) =>
      (value(a, sort.key) - value(b, sort.key)) * sort.dir ||
      a.name.localeCompare(b.name)
  );
  const maxRows = Math.max(1, ...rows.map((r) => r.rows));
  const cols: { key: SortKey; label: string }[] = [
    { key: 'runs', label: t('runs') },
    { key: 'rate', label: t('rate') },
    { key: 'rows', label: t('rows') },
    { key: 'median', label: t('median') },
  ];
  const lastDot: Record<string, string> = {
    succeeded: 'bg-[var(--viz-ok)]',
    failed: 'bg-[var(--viz-fail)]',
    canceled: 'bg-[var(--viz-cancel)]',
  };

  return (
    <Panel className="flex flex-col gap-2.5 px-5 pb-2 pt-[18px]">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="m-0 text-[15px] font-semibold">{t('title')}</h2>
        <span className="text-[12.5px] text-muted-foreground">{t('hint')}</span>
      </div>
      {rows.length === 0 ? (
        <p className="m-0 py-6 text-center text-sm text-muted-foreground">
          {t('empty')}
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] border-collapse text-[13px]">
            <thead>
              <tr className="text-left text-xs text-muted-foreground">
                <th className="px-2.5 py-2 font-medium">{t('job')}</th>
                {cols.map((c) => (
                  <th
                    key={c.key}
                    aria-sort={
                      sort.key === c.key
                        ? sort.dir < 0
                          ? 'descending'
                          : 'ascending'
                        : undefined
                    }
                    className={cn(
                      'px-2.5 py-2 text-right',
                      sort.key === c.key
                        ? 'font-semibold text-foreground'
                        : 'font-medium'
                    )}
                  >
                    <button
                      type="button"
                      onClick={() =>
                        setSort((s) => ({
                          key: c.key,
                          dir: s.key === c.key ? (s.dir === 1 ? -1 : 1) : -1,
                        }))
                      }
                      className="inline-flex cursor-pointer items-center gap-1"
                    >
                      {c.label}
                      <span aria-hidden="true">
                        {sort.key === c.key ? (sort.dir < 0 ? '↓' : '↑') : ''}
                      </span>
                    </button>
                  </th>
                ))}
                <th className="px-2.5 py-2 font-medium">{t('last')}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr
                  key={r.jobId}
                  className="border-t border-line-soft hover:bg-hover"
                >
                  <td className="px-2.5 py-[11px]">
                    <span className="flex items-center gap-2">
                      <Link
                        href={r.href}
                        className="font-semibold hover:underline"
                      >
                        {r.name}
                      </Link>
                      <span className="rounded-full bg-surface-2 px-[7px] py-0.5 text-[11px] text-quiet-fg">
                        {r.kind}
                      </span>
                    </span>
                  </td>
                  <td className="px-2.5 py-[11px] text-right tabular-nums">
                    {format.number(r.runs)}
                  </td>
                  <td className="px-2.5 py-[11px]">
                    <span className="flex items-center justify-end gap-2">
                      <InlineBar percent={r.rate ?? 0} width="w-[70px]" />
                      <span className="w-11 text-right tabular-nums">
                        {r.rate === undefined
                          ? '—'
                          : t('percent', { value: Math.round(r.rate) })}
                      </span>
                    </span>
                  </td>
                  <td className="px-2.5 py-[11px]">
                    <span className="flex items-center justify-end gap-2">
                      <InlineBar
                        percent={(r.rows / maxRows) * 100}
                        width="w-[110px]"
                      />
                      <span className="w-14 text-right tabular-nums">
                        {format.number(r.rows, { notation: 'compact' })}
                      </span>
                    </span>
                  </td>
                  <td className="px-2.5 py-[11px] text-right tabular-nums">
                    {r.medianSeconds === undefined
                      ? '—'
                      : formatSpan(r.medianSeconds * 1000)}
                  </td>
                  <td className="px-2.5 py-[11px]">
                    {r.last ? (
                      <span className="inline-flex items-center gap-1.5 text-[12.5px]">
                        <span
                          className={cn(
                            'size-2 rounded-full',
                            lastDot[r.last.outcome]
                          )}
                          aria-hidden="true"
                        />
                        {r.last.label}
                      </span>
                    ) : (
                      <span className="text-muted-foreground">—</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  );
}

function InlineBar(props: { percent: number; width: string }): ReactElement {
  return (
    <span
      className={cn(
        'h-1.5 overflow-hidden rounded-full bg-surface-2',
        props.width
      )}
      aria-hidden="true"
    >
      <span
        className="block h-full rounded-full bg-[var(--viz-ok)]"
        style={{ width: `${Math.max(0, Math.min(100, props.percent))}%` }}
      />
    </span>
  );
}
