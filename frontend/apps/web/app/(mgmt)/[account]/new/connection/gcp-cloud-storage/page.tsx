'use client';
import GcpCloudStorageConnectionForm from '@/components/connections/forms/gcp-cloud-storage/GcpCloudStorageConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { SiGooglecloud } from 'react-icons/si';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';

export default function NewGCPCloudStoragePage(): ReactElement {
  const t = useTranslations('connections.newConnection.subHeadings');
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="GCP Cloud Storage"
      subHeadings={t('gcpCloudStorage')}
      leftIcon={<SiGooglecloud className="w-[40px] h-[40px]" />}
    >
      <GcpCloudStorageConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
