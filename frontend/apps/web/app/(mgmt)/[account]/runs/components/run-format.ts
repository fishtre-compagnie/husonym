import { Timestamp, timestampDate } from '@bufbuild/protobuf/wkt';
import { formatDistanceToNowStrict } from 'date-fns';
import { useEffect, useState } from 'react';

/** A span of time as a person reads it: 350 ms, 42 s, 7 min 42 s, 1 h 03 min. */
export function formatSpan(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) {
    return '–';
  }
  if (ms < 1000) {
    return `${Math.round(ms)} ms`;
  }
  const totalSeconds = Math.floor(ms / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours} h ${String(minutes).padStart(2, '0')} min`;
  }
  if (minutes > 0) {
    return `${minutes} min ${String(seconds).padStart(2, '0')} s`;
  }
  if (ms < 10_000) {
    return `${(ms / 1000).toFixed(1)} s`;
  }
  return `${seconds} s`;
}

export function toDate(ts?: Timestamp): Date | undefined {
  return ts ? timestampDate(ts) : undefined;
}

/** How long a run lasted, or has lasted so far when it has no end yet. */
export function runSpan(
  startedAt?: Timestamp,
  completedAt?: Timestamp,
  now: Date = new Date()
): number | undefined {
  const start = toDate(startedAt);
  if (!start) {
    return undefined;
  }
  const end = toDate(completedAt) ?? now;
  return end.getTime() - start.getTime();
}

export function formatAgo(date?: Date): string {
  if (!date) {
    return '–';
  }
  return `${formatDistanceToNowStrict(date)} ago`;
}

/** The current time, ticking every `everyMs` while `active`. */
export function useNow(active: boolean, everyMs = 1000): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    if (!active) {
      return;
    }
    const t = setInterval(() => setNow(new Date()), everyMs);
    return () => clearInterval(t);
  }, [active, everyMs]);
  return now;
}
