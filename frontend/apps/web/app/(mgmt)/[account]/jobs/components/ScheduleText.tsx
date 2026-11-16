'use client';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { ScheduleLabel } from './schedule-label';

// ScheduleText says a schedule label: its phrase in the UI's language, or the cron
// expression as it is.
export function ScheduleText({
  label,
}: {
  label: ScheduleLabel;
}): ReactElement {
  const t = useTranslations('jobs.schedule');
  return <>{label.isCron ? label.text : t(label.key, label.values)}</>;
}
