'use client';
import AwsS3ConnectionForm from '@/components/connections/forms/s3/AwsS3ConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { FaAws } from 'react-icons/fa';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';

export default function NewAwsS3ConnectionPage(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="AWS S3"
      subHeadings={t('awsS3')}
      leftIcon={<FaAws className="w-[40px] h-[40px]" />}
    >
      <AwsS3ConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
