'use client';
import MongoDbConnectionForm from '@/components/connections/forms/mongodb/MongoDbConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { DiMongodb } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
export default function NewMongoDBConnectionPage(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="MongoDB"
      subHeadings={t('mongodb')}
      leftIcon={<DiMongodb className=" w-[40px] h-[40px]" />}
    >
      <MongoDbConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
