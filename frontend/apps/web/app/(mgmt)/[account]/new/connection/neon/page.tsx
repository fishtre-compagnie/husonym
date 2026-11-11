'use client';
import PostgresConnectionForm from '@/components/connections/forms/postgres/PostgresConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { ReactElement } from 'react';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
import { NeonLogo } from './NeonLogo';

export default function NewPostgresNeonPage(): ReactElement {
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="Neon"
      subHeadings="Configure a Neon database as a connection"
      leftIcon={<NeonLogo />}
    >
      <PostgresConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
