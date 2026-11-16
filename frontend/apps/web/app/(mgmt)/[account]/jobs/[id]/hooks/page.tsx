'use client';

import { PageProps } from '@/components/types';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Skeleton } from '@/components/ui/skeleton';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { ReactElement, use } from 'react';
import HooksCard from './components/HooksCard';
import { useTranslations } from 'next-intl';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  const { data: configData, isLoading } = useGetSystemAppConfig();
  if (isLoading) {
    return <Skeleton className="w-full h-12" />;
  }
  if (!configData?.isJobHooksEnabled) {
    return <HooksDisabledAlert />;
  }
  return (
    <div className="job-hooks-page-container">
      <HooksCard jobId={id} />
    </div>
  );
}

function HooksDisabledAlert(): ReactElement {
  const t = useTranslations('jobs.hooks.disabled');
  return (
    <div>
      <Alert variant="warning">
        <AlertTitle>{t('title')}</AlertTitle>
        <AlertDescription>{t('description')}</AlertDescription>
      </Alert>
    </div>
  );
}
