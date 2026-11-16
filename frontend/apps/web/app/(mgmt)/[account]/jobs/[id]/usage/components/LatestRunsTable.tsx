import JobRunStatus from '@/app/(mgmt)/[account]/runs/components/JobRunStatus';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { runsTable } from '@/libs/usage/rows';
import { GetJobUsageResponse } from '@husonym/sdk';
import Link from 'next/link';
import { ReactElement } from 'react';
import { useUsageI18n } from '@/libs/usage/useUsageI18n';
import { useTranslations } from 'next-intl';
import { useFormatDateTime } from '@/i18n/date-fns';

interface Props {
  accountName: string;
  // The usage of the job: its latest runs, its kind and what its runs add up to.
  usage: GetJobUsageResponse;
  // What to say in place of the table, when the period has no run to list.
  emptyLine?: string;
}

// The latest runs of a job in the period, the most recent first. A run leads to its page.
export default function LatestRunsTable(props: Props): ReactElement {
  const { accountName, usage, emptyLine } = props;
  const t = useTranslations('jobs.usage.latestRuns');
  const formatDateTime = useFormatDateTime();
  const { rows, caption } = runsTable(usage, accountName, useUsageI18n());
  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('title')}</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {emptyLine ? (
          <p className="text-sm text-muted-foreground">{emptyLine}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead scope="col" className="px-2">
                  {t('run')}
                </TableHead>
                <TableHead scope="col" className="px-2">
                  {t('started')}
                </TableHead>
                <TableHead scope="col" className="px-2 text-right">
                  {t('duration')}
                </TableHead>
                <TableHead scope="col" className="px-2">
                  {t('status')}
                </TableHead>
                <TableHead scope="col" className="px-2 text-right">
                  {t('rowsRead')}
                </TableHead>
                <TableHead scope="col" className="px-2">
                  {t('error')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.id}>
                  <TableCell>
                    <Link href={row.href} className="hover:underline">
                      {row.id}
                    </Link>
                  </TableCell>
                  <TableCell>{formatDateTime(row.startedAt)}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {row.duration}
                  </TableCell>
                  <TableCell>
                    <JobRunStatus
                      status={row.status}
                      containerClassName="px-0"
                    />
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {row.rowsRead}
                  </TableCell>
                  <TableCell>{row.error}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {caption && <p className="text-xs text-muted-foreground">{caption}</p>}
      </CardContent>
    </Card>
  );
}
