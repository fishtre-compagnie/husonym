'use client';
import { TanstackQueryProviderIgnore404Errors } from '@/components/providers/query-provider';
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';
import { useQuery } from '@connectrpc/connect-query';
import { Code, JobService } from '@husonym/sdk';
import { ReactElement } from 'react';
import PreflightReportView from './PreflightReportView';
import {
  countReport,
  engineLabel,
  parseKeptReport,
  PREFLIGHT_REPORT_EXTERNAL_ID,
  summarize,
} from './report';
import { useJobConnections } from './useJobConnections';

interface Props {
  jobRunId: string;
  accountId: string;
  jobId: string;
  // While the run goes on, its report may not be kept yet: it is asked again.
  isRunning: boolean;
}

// JobRunPreflight shows what the run found at its start, as it kept it. It is open when the
// run stopped on it, folded otherwise. A run started before the check existed, a PII
// detection run, or one stopped before its check, kept none: nothing is shown, and the
// 404 is no error.
//
// What is kept is shown for reading. Where the API does not keep run contexts to workers,
// someone editing the job may have written it: the statements granting what is missing
// are not offered for copy, the check of the job gives them.
export default function JobRunPreflight(props: Props): ReactElement {
  return (
    <TanstackQueryProviderIgnore404Errors>
      <KeptReport {...props} />
    </TanstackQueryProviderIgnore404Errors>
  );
}

function KeptReport(props: Props): ReactElement | null {
  const { jobRunId, accountId, jobId, isRunning } = props;
  const { data: jobData } = useQuery(
    JobService.method.getJob,
    { id: jobId },
    { enabled: !!jobId }
  );
  const { data, error } = useQuery(
    JobService.method.getRunContext,
    { id: { jobRunId, externalId: PREFLIGHT_REPORT_EXTERNAL_ID, accountId } },
    {
      enabled: !!jobRunId && !!accountId,
      retry: false,
      refetchInterval: (query) =>
        isRunning && !query.state.data ? 5000 : false,
    }
  );
  const connections = useJobConnections(jobData?.job, accountId);

  if (error?.code === Code.NotFound || !data?.value) {
    return null;
  }
  const report = parseKeptReport(data.value);
  if (!report) {
    return null;
  }
  const counts = countReport(report);
  return (
    <Accordion
      type="single"
      collapsible
      defaultValue={counts.blocking > 0 ? 'preflight' : undefined}
    >
      <AccordionItem value="preflight">
        <AccordionTrigger>
          <div className="flex flex-col items-start gap-1 text-left">
            <span className="font-semibold">Pre-flight of this run</span>
            <span className="text-sm font-normal text-muted-foreground">
              {engineLabel(report.engine)} · {summarize(counts)}
            </span>
          </div>
        </AccordionTrigger>
        <AccordionContent>
          <PreflightReportView
            report={report}
            connections={connections}
            copyableRemedies={false}
          />
          <p className="pt-3 text-sm text-muted-foreground">
            The statements that grant what is missing are given by the
            pre-flight check of the job.
          </p>
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  );
}
