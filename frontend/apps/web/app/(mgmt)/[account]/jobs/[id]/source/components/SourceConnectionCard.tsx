'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { useQuery } from '@connectrpc/connect-query';
import { JobService } from '@husonym/sdk';
import { ReactElement } from 'react';
import { isAiDataGenJob, isDataGenJob, isPiiDetectJob } from '../../util';
import AiDataGenConnectionCard from './AiDataGenConnectionCard';
import DataGenConnectionCard from './DataGenConnectionCard';
import DataSyncConnectionCard, {
  SourceSection,
} from './DataSyncConnectionCard';
import PiiDetectConnectionCard from './PiiDetectConnectionCard';
import SchemaPageSkeleton from './SchemaPageSkeleton';

interface Props {
  jobId: string;
  // A sync job shows its source and its mapping on two tabs; the other kinds
  // of job show both on their Source tab.
  section: SourceSection;
}

export default function SourceConnectionCard({
  jobId,
  section,
}: Props): ReactElement {
  const { data, isLoading } = useQuery(
    JobService.method.getJob,
    { id: jobId },
    { enabled: !!jobId }
  );

  if (isLoading) {
    return <SchemaPageSkeleton />;
  }
  const job = data?.job;
  if (isDataGenJob(job) || isAiDataGenJob(job) || isPiiDetectJob(job)) {
    return (
      <Section
        header="Source"
        description="Where the data comes from, and what the job does with it. Changes are kept once you save them."
      >
        {isDataGenJob(job) ? (
          <DataGenConnectionCard jobId={jobId} />
        ) : isAiDataGenJob(job) ? (
          <AiDataGenConnectionCard jobId={jobId} />
        ) : (
          <PiiDetectConnectionCard jobId={jobId} />
        )}
      </Section>
    );
  }
  return section === 'source' ? (
    <Section
      header="Source"
      description="Where the data comes from, and what a run does when its schema changes. Changes are kept once you save them."
    >
      <DataSyncConnectionCard jobId={jobId} section="source" />
    </Section>
  ) : (
    <Section
      header="Mapping"
      description="The tables the job copies, and what leaves each of their columns. Changes are kept once you save them."
    >
      <DataSyncConnectionCard jobId={jobId} section="mapping" />
    </Section>
  );
}

function Section(props: {
  header: string;
  description: string;
  children: ReactElement;
}): ReactElement {
  return (
    <div className="job-details-container flex flex-col gap-5">
      <SubPageHeader header={props.header} description={props.description} />
      {props.children}
    </div>
  );
}
