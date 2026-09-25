'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { formatDateTime } from '@/util/util';
import { Job } from '@husonym/sdk';
import { ReactElement } from 'react';
import PreflightReportView from './PreflightReportView';
import { countReport, engineLabel, summarize } from './report';
import { useJobConnections } from './useJobConnections';
import { usePreflightCheck } from './usePreflightCheck';

interface Props {
  job: Job;
}

// PreflightCard tells, when asked, what a run of the job would meet. It asks nothing by
// itself: each check logs in to the databases of the job.
export default function PreflightCard(props: Props): ReactElement {
  const { job } = props;
  const { check, isChecking, outcome } = usePreflightCheck(job.id);
  const connections = useJobConnections(job, job.accountId);
  const report = outcome?.report;

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-4 space-y-0">
        <div className="flex flex-col gap-1.5">
          <CardTitle>Pre-flight</CardTitle>
          <CardDescription>
            {report && outcome?.checkedAt
              ? `${engineLabel(report.engine)} · checked ${formatDateTime(outcome.checkedAt)}`
              : 'What a run of this job would meet, found from the plan of the run and the rights of its connections. Nothing is read from the tables nor written.'}
          </CardDescription>
        </div>
        <Button
          type="button"
          variant="outline"
          disabled={isChecking}
          onClick={() => void check()}
        >
          <ButtonText
            leftIcon={isChecking ? <Spinner /> : undefined}
            text={isChecking ? 'Checking…' : 'Check now'}
          />
        </Button>
      </CardHeader>
      {isChecking || outcome ? (
        <CardContent className="flex flex-col gap-4">
          {isChecking ? (
            <p className="text-sm text-muted-foreground">
              A worker computes the plan of the run and asks the connections: it
              takes a few seconds, up to a minute on a large schema.
            </p>
          ) : null}
          {!isChecking && outcome?.error ? (
            <Alert variant="destructive">
              <AlertTitle>The check did not end</AlertTitle>
              <AlertDescription>{outcome.error}</AlertDescription>
            </Alert>
          ) : null}
          {!isChecking && report ? (
            <>
              <p className="text-sm">{summarize(countReport(report))}</p>
              <PreflightReportView report={report} connections={connections} />
            </>
          ) : null}
        </CardContent>
      ) : null}
    </Card>
  );
}
