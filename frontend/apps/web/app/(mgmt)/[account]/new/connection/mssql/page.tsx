'use client';
import SqlServerConnectionForm from '@/components/connections/forms/sql-server/SqlServerConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { DiMysql } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
export default function Mssql() {
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="Microsoft SQL Server"
      subHeadings="Configure a Microsoft SQL Server database as a connection"
      leftIcon={<DiMysql className="w-[40px] h-[40px]" />}
    >
      <SqlServerConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
