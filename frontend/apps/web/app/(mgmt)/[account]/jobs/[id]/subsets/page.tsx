'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { PageProps } from '@/components/types';
import { ReactElement, use } from 'react';
import SubsetCard from './components/SubsetCard';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  return (
    <div className="job-details-container flex flex-col gap-5">
      <SubPageHeader
        header="Subsets"
        description="Which rows of each table the job copies, with a WHERE clause per table."
      />
      <SubsetCard jobId={id} />
    </div>
  );
}
