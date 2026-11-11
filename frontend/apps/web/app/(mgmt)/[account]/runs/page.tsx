'use client';
import { PageBody } from '@/components/ds';
import { ReactElement } from 'react';
import RunsTable from './components/RunsTable';

export default function JobRuns(): ReactElement {
  return (
    <PageBody className="runs-page">
      <RunsTable />
    </PageBody>
  );
}
