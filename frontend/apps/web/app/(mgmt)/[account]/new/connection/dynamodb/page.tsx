'use client';
import DynamoDbConnectionForm from '@/components/connections/forms/dynamodb/DynamoDbConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { ReactElement } from 'react';
import { FaAws } from 'react-icons/fa';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';

export default function NewDynamoDBConnection(): ReactElement {
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="DynamoDB"
      subHeadings="Configure DynamoDB as a connection"
      leftIcon={<FaAws className="w-[40px] h-[40px]" />}
    >
      <DynamoDbConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
