import { TanstackQueryProviderIgnore404Errors } from '@/components/providers/query-provider';
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';
import { Panel, PanelHeader } from '@/components/ds';
import { useQuery } from '@connectrpc/connect-query';
import { Code, Connection, ConnectionService, JobService } from '@husonym/sdk';
import { ReactElement } from 'react';
import { IoAlertCircleOutline } from 'react-icons/io5';

interface JobRunActivityErrorsProps {
  jobRunId: string;
  jobId: string;
  accountId: string;
}

interface InitSchemaReport {
  ConnectionId: string;
  Errors: { Statement: string; Error: string }[];
}

function parseUint8ArrayToInitSchemaReport(
  data: Uint8Array
): InitSchemaReport[] | null {
  try {
    const jsonString = new TextDecoder().decode(data);
    const parsedData: InitSchemaReport[] = JSON.parse(jsonString);
    return parsedData;
  } catch (error) {
    console.error('Error parsing JSON:', error);
    return null;
  }
}

function parseUint8ArrayToReconcileSchemaReport(
  data: Uint8Array
): InitSchemaReport[] | null {
  try {
    const jsonString = new TextDecoder().decode(data);
    const parsedData: InitSchemaReport = JSON.parse(jsonString);
    return [parsedData];
  } catch (error) {
    console.error('Error parsing JSON:', error);
    return null;
  }
}

export default function JobRunActivityErrors(
  props: JobRunActivityErrorsProps
): ReactElement {
  const { data: jobData } = useQuery(
    JobService.method.getJob,
    { id: props.jobId },
    { enabled: !!props.jobId }
  );

  const job = jobData?.job;
  return (
    <TanstackQueryProviderIgnore404Errors>
      <JobRunInitSchemaErrorViewer
        jobRunId={props.jobRunId}
        accountId={props.accountId}
        externalId="init-schema-report"
        parseUint8ArrayToInitSchemaReport={parseUint8ArrayToInitSchemaReport}
      />
      {job?.destinations?.map((destination) => (
        <JobRunInitSchemaErrorViewer
          key={destination.id}
          jobRunId={props.jobRunId}
          accountId={props.accountId}
          externalId={`init-schema-report-${destination.id}`}
          parseUint8ArrayToInitSchemaReport={parseUint8ArrayToInitSchemaReport}
        />
      ))}
      {job?.destinations?.map((destination) => (
        <JobRunInitSchemaErrorViewer
          key={destination.id}
          jobRunId={props.jobRunId}
          accountId={props.accountId}
          externalId={`reconcile-schema-report-${destination.id}`}
          parseUint8ArrayToInitSchemaReport={
            parseUint8ArrayToReconcileSchemaReport
          }
        />
      ))}
    </TanstackQueryProviderIgnore404Errors>
  );
}

interface JobRunInitSchemaErrorViewerProps {
  jobRunId: string;
  accountId: string;
  externalId: string;
  parseUint8ArrayToInitSchemaReport: (
    data: Uint8Array
  ) => InitSchemaReport[] | null;
}

function JobRunInitSchemaErrorViewer(
  props: JobRunInitSchemaErrorViewerProps
): ReactElement {
  const { jobRunId, accountId, externalId, parseUint8ArrayToInitSchemaReport } =
    props;

  const { data: runContextData, error: runContextError } = useQuery(
    JobService.method.getRunContext,
    {
      id: {
        jobRunId,
        externalId,
        accountId: accountId,
      },
    },
    { enabled: !!jobRunId && !!accountId, retry: false }
  );

  const { data: connectionsData } = useQuery(
    ConnectionService.method.getConnections,
    { accountId },
    { enabled: !!accountId }
  );

  if (runContextError && runContextError.code === Code.NotFound) {
    return <div></div>;
  }

  const runContext = runContextData?.value
    ? parseUint8ArrayToInitSchemaReport(runContextData.value)
    : null;

  const filteredRunContext = runContext?.filter(
    (item) => item.Errors && item.Errors.length > 0
  );

  const connectionsMap =
    connectionsData?.connections?.reduce(
      (acc, connection) => {
        acc[connection.id] = connection;
        return acc;
      },
      {} as Record<string, Connection>
    ) ?? {};

  if (!filteredRunContext || filteredRunContext.length === 0) {
    return <></>;
  }

  return (
    <Panel tone="danger" aria-label="Schema initialization errors">
      <PanelHeader
        leading={
          <IoAlertCircleOutline
            aria-hidden="true"
            className="h-5 w-5 text-danger-fg"
          />
        }
        title="Schema initialization errors"
      />
      <div className="max-h-[400px] overflow-y-auto">
        {filteredRunContext.map((connectionError) => (
          <div
            key={connectionError.ConnectionId}
            className="border-b border-line-soft px-5 py-4 last:border-b-0"
          >
            <p className="m-0 flex flex-wrap items-baseline gap-x-3 font-medium">
              {connectionsMap[connectionError.ConnectionId]?.name ??
                'Unknown connection'}
              <span className="font-mono text-xs text-muted-foreground">
                {connectionError.ConnectionId}
              </span>
            </p>
            <Accordion type="single" collapsible className="w-full">
              {connectionError.Errors.map((error, errorIdx) => (
                <AccordionItem
                  value={`item-${errorIdx}`}
                  key={errorIdx}
                  className="border-line-soft"
                >
                  <AccordionTrigger className="text-left text-[13px] text-danger-fg">
                    {error.Error}
                  </AccordionTrigger>
                  <AccordionContent>
                    <div className="rounded-lg bg-surface-2 p-3">
                      <p className="m-0 text-xs text-muted-foreground">
                        Statement
                      </p>
                      <pre className="mt-2 whitespace-pre-wrap break-words font-mono text-xs">
                        {error.Statement}
                      </pre>
                    </div>
                  </AccordionContent>
                </AccordionItem>
              ))}
            </Accordion>
          </div>
        ))}
      </div>
    </Panel>
  );
}
