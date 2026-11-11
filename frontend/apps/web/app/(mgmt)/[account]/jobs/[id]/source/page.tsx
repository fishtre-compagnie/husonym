'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { PageProps } from '@/components/types';
import { ReactElement, use } from 'react';
import SourceConnectionCard from './components/SourceConnectionCard';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  return (
    <div className="job-details-container flex flex-col gap-5">
      <SubPageHeader
        header="Source & mapping"
        description="Where the data comes from, and what leaves each column. Changes are kept once you save them."
      />

      <div className="flex flex-col gap-10">
        <SourceConnectionCard jobId={id} />
      </div>
    </div>
  );
}
