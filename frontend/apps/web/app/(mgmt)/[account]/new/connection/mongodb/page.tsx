'use client';
import MongoDbConnectionForm from '@/components/connections/forms/mongodb/MongoDbConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { ReactElement } from 'react';
import { DiMongodb } from 'react-icons/di';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
export default function NewMongoDBConnectionPage(): ReactElement {
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="MongoDB"
      subHeadings="Configure a MongoDB database as a connection"
      leftIcon={<DiMongodb className=" w-[40px] h-[40px]" />}
    >
      <MongoDbConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
