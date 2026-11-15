'use client';
import { PageProps } from '@/components/types';
import { ReactElement, use } from 'react';
import SourceConnectionCard from './components/SourceConnectionCard';

export default function Page(props: PageProps): ReactElement {
  const params = use(props.params);
  return <SourceConnectionCard jobId={params?.id ?? ''} section="source" />;
}
