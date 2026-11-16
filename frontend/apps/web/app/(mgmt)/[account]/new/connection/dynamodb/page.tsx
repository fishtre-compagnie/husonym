'use client';
import DynamoDbConnectionForm from '@/components/connections/forms/dynamodb/DynamoDbConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { FaAws } from 'react-icons/fa';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';

export default function NewDynamoDBConnection(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="DynamoDB"
      subHeadings={t('dynamodb')}
      leftIcon={<FaAws className="w-[40px] h-[40px]" />}
    >
      <DynamoDbConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
