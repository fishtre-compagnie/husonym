'use client';

import { PageProps } from '@/components/types';
import { ReactElement, use } from 'react';
import PendingReviewCard from './components/PendingReviewCard';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  const id = params?.id ?? '';
  return (
    <div className="job-review-page-container">
      <PendingReviewCard jobId={id} />
    </div>
  );
}
