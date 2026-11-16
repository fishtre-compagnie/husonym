'use client';
import MysqlConnectionForm from '@/components/connections/forms/mysql/MysqlConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { DiMysql } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
export default function NewMysqlConnectionPage(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();

  return (
    <NewConnectionLayout
      header="MySQL"
      subHeadings={t('mysql')}
      leftIcon={<DiMysql className="w-[40px] h-[40px]" />}
    >
      <MysqlConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
