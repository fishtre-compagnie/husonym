'use client';
import PostgresConnectionForm from '@/components/connections/forms/postgres/PostgresConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
import { SupabaseLogo } from './SupabaseLogo';

export default function NewSupabasePage(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="Supabase"
      subHeadings={t('supabase')}
      leftIcon={<SupabaseLogo />}
    >
      <PostgresConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
