'use client';
import { PageBody, PageHeader } from '@/components/ds';
import { useSearchParams } from 'next/navigation';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { useGetConnectionsMetadata } from '../../connections/util';
import ConnectionCard from './components/ConnectionCard';

export default function NewConnectionPage(): ReactElement {
  const t = useTranslations('connections.newConnection');
  const searchParams = useSearchParams();
  const connectionTypes = new Set(searchParams.getAll('connectionType'));

  const connections = useGetConnectionsMetadata(connectionTypes);
  return (
    <PageBody>
      <PageHeader title={t('title')} description={t('description')} />
      <div
        role="list"
        aria-label={t('typesLabel')}
        className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-3"
      >
        {connections.map((connection) => (
          <div role="listitem" key={connection.urlSlug}>
            <ConnectionCard connection={connection} />
          </div>
        ))}
      </div>
    </PageBody>
  );
}
