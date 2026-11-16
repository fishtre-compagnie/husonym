'use client';
import PostgresConnectionForm from '@/components/connections/forms/postgres/PostgresConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { DiPostgresql } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';

export default function NewPostgresPage(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="PostgreSQL"
      subHeadings={t('postgres')}
      leftIcon={<DiPostgresql className=" w-[40px] h-[40px]" />}
    >
      <PostgresConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
