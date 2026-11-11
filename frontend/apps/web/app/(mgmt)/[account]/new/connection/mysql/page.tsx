'use client';
import MysqlConnectionForm from '@/components/connections/forms/mysql/MysqlConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { ReactElement } from 'react';
import { DiMysql } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
export default function NewMysqlConnectionPage(): ReactElement {
  const onSuccess = useOnCreateSuccess();

  return (
    <NewConnectionLayout
      header="MySQL"
      subHeadings="Configure a MySQL database as a connection"
      leftIcon={<DiMysql className="w-[40px] h-[40px]" />}
    >
      <MysqlConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
