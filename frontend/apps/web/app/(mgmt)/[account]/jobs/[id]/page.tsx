'use client';
import { PageProps } from '@/components/types';

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';
import JobNotFoundAlert from '@/components/jobs/JobNotFoundAlert';
import PreflightCard from '@/components/jobs/preflight/PreflightCard';
import { hasPreflight } from '@/components/jobs/preflight/report';
import { create } from '@bufbuild/protobuf';
import { createConnectQueryKey, useQuery } from '@connectrpc/connect-query';
import { GetJobResponseSchema, JobService } from '@husonym/sdk';
import { useQueryClient } from '@tanstack/react-query';
import { ReactElement, use } from 'react';
import ActivitySyncOptionsCard from './components/ActivitySyncOptionsCard';
import JobNextRuns from './components/NextRuns';
import JobRecentRuns from './components/RecentRuns';
import JobScheduleCard from './components/ScheduleCard';
import WorkflowSettingsCard from './components/WorkflowSettingsCard';
import JobIdSkeletonForm from './JobIdSkeletonForm';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  const { data, isLoading } = useQuery(
    JobService.method.getJob,
    { id },
    { enabled: !!id }
  );
  const { data: jobStatus } = useQuery(
    JobService.method.getJobStatus,
    { jobId: id },
    { enabled: !!id }
  );
  const queryclient = useQueryClient();

  if (isLoading) {
    return <JobIdSkeletonForm />;
  }

  if (!data?.job) {
    return <JobNotFoundAlert />;
  }

  return (
    <div className="job-details-container">
      <div className="flex flex-col gap-5">
        <div className="grid grid-cols-1 gap-5 lg:grid-cols-[minmax(0,3fr)_minmax(260px,1fr)]">
          <div className="min-w-0">
            <JobScheduleCard
              job={data.job}
              mutate={(newjob) => {
                queryclient.setQueryData(
                  createConnectQueryKey({
                    schema: JobService.method.getJob,
                    input: { id },
                    cardinality: undefined,
                  }),
                  create(GetJobResponseSchema, { job: newjob })
                );
              }}
            />
          </div>
          <div className="min-w-0">
            <JobNextRuns jobId={id} status={jobStatus?.status} />
          </div>
        </div>
        {hasPreflight(data.job) ? <PreflightCard job={data.job} /> : null}
        <JobRecentRuns jobId={id} />
        <Accordion type="single" collapsible>
          <AccordionItem
            value="advanced-settings"
            className="rounded-2xl border border-line-soft bg-card px-5"
          >
            <AccordionTrigger className="font-display text-base font-semibold hover:no-underline">
              Advanced settings
            </AccordionTrigger>
            <AccordionContent>
              <div className="flex flex-col gap-3 pb-2">
                <div>
                  <WorkflowSettingsCard
                    job={data.job}
                    mutate={(newjob) => {
                      queryclient.setQueryData(
                        createConnectQueryKey({
                          schema: JobService.method.getJob,
                          input: { id },
                          cardinality: undefined,
                        }),
                        create(GetJobResponseSchema, { job: newjob })
                      );
                    }}
                  />
                </div>
                <div>
                  <ActivitySyncOptionsCard
                    job={data.job}
                    mutate={(newjob) => {
                      queryclient.setQueryData(
                        createConnectQueryKey({
                          schema: JobService.method.getJob,
                          input: { id },
                          cardinality: undefined,
                        }),
                        create(GetJobResponseSchema, { job: newjob })
                      );
                    }}
                  />
                </div>
              </div>
            </AccordionContent>
          </AccordionItem>
        </Accordion>
      </div>
    </div>
  );
}
