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
import { formatDateTime } from '@/util/util';
import { GetJobUsageResponse } from '@husonym/sdk';
import Link from 'next/link';
import { ReactElement } from 'react';

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
  const { rows, caption } = runsTable(usage, accountName);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Latest runs</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {emptyLine ? (
          <p className="text-sm text-muted-foreground">{emptyLine}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Run</TableHead>
                <TableHead scope="col">Started</TableHead>
                <TableHead scope="col" className="text-right">
                  Duration
                </TableHead>
                <TableHead scope="col">Status</TableHead>
                <TableHead scope="col" className="text-right">
                  Rows read
                </TableHead>
                <TableHead scope="col">Error</TableHead>
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
