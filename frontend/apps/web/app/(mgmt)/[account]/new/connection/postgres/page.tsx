'use client';
import PostgresConnectionForm from '@/components/connections/forms/postgres/PostgresConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { ReactElement } from 'react';
import { DiPostgresql } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';

export default function NewPostgresPage(): ReactElement {
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="PostgreSQL"
      subHeadings="Configure a PostgreSQL database as a connection"
      leftIcon={<DiPostgresql className=" w-[40px] h-[40px]" />}
    >
      <PostgresConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
