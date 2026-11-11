'use client';
import PostgresConnectionForm from '@/components/connections/forms/postgres/PostgresConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { ReactElement } from 'react';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
import { SupabaseLogo } from './SupabaseLogo';

export default function NewSupabasePage(): ReactElement {
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="Supabase"
      subHeadings="Configure a Supabase database as a connection"
      leftIcon={<SupabaseLogo />}
    >
      <PostgresConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
