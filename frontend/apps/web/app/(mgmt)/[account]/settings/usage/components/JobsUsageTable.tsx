import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { jobsEmptyLine, jobsTable } from '@/libs/usage/rows';
import { JobUsage, UsageTotals } from '@husonym/sdk';
import Link from 'next/link';
import { ReactElement } from 'react';

interface Props {
  accountName: string;
  // The jobs that ran in the period and still exist, as the API orders them.
  jobs: readonly JobUsage[];
  // The totals of the account, which also count the runs of jobs deleted since.
  totals?: UsageTotals;
}

// What each job of the account ran in the period. A row leads to the usage of its job.
export default function JobsUsageTable(props: Props): ReactElement {
  const { accountName, jobs, totals } = props;
  const { rows, note } = jobsTable(jobs, accountName);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Jobs</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            {jobsEmptyLine(totals)}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">Job</TableHead>
                <TableHead scope="col" className="text-right">
                  Rows read
                </TableHead>
                <TableHead scope="col" className="text-right">
                  Runs
                </TableHead>
                <TableHead scope="col" className="text-right">
                  Success
                </TableHead>
                <TableHead scope="col" className="text-right">
                  Median duration
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.id}>
                  <TableCell>
                    <Link
                      href={row.href}
                      className="hover:underline"
                      aria-label={`Usage of the job ${row.name}`}
                    >
                      {row.name}
                    </Link>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {row.rowsRead}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {row.runs}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {row.success}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">
                    {row.duration}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {note && <p className="text-xs text-muted-foreground">{note}</p>}
      </CardContent>
    </Card>
  );
}
