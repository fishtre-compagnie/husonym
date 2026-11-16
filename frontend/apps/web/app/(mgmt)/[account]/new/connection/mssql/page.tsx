'use client';
import SqlServerConnectionForm from '@/components/connections/forms/sql-server/SqlServerConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { DiMysql } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
export default function Mssql() {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="Microsoft SQL Server"
      subHeadings={t('mssql')}
      leftIcon={<DiMysql className="w-[40px] h-[40px]" />}
    >
      <SqlServerConnectionForm onSuccess={onSuccess} mode="create" />
    </NewConnectionLayout>
  );
}
