'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { PageProps } from '@/components/types';
import { ReactElement, use } from 'react';
import SubsetCard from './components/SubsetCard';
import { useTranslations } from 'next-intl';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  const t = useTranslations('jobs.subsets');
  return (
    <div className="job-details-container flex flex-col gap-5">
      <SubPageHeader header={t('title')} description={t('description')} />
      <SubsetCard jobId={id} />
    </div>
  );
}
