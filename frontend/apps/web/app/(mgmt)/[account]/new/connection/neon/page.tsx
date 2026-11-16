'use client';
import PostgresConnectionForm from '@/components/connections/forms/postgres/PostgresConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
import { NeonLogo } from './NeonLogo';

export default function NewPostgresNeonPage(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="Neon"
      subHeadings={t('neon')}
      leftIcon={<NeonLogo />}
    >
      <PostgresConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
