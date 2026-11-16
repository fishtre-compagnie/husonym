'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { useQuery } from '@connectrpc/connect-query';
import { JobService } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
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
  const t = useTranslations('jobs.source');
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
      <Section header={t('title')} description={t('otherDescription')}>
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
    <Section header={t('title')} description={t('syncDescription')}>
      <DataSyncConnectionCard jobId={jobId} section="source" />
    </Section>
  ) : (
    <Section header={t('mappingTitle')} description={t('mappingDescription')}>
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
