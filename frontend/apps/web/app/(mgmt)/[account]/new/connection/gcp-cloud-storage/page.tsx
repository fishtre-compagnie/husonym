'use client';
import GcpCloudStorageConnectionForm from '@/components/connections/forms/gcp-cloud-storage/GcpCloudStorageConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { ReactElement } from 'react';
import { SiGooglecloud } from 'react-icons/si';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';

export default function NewGCPCloudStoragePage(): ReactElement {
  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="GCP Cloud Storage"
      subHeadings="Configure a GCP Cloud Storage bucket as a connection"
      leftIcon={<SiGooglecloud className="w-[40px] h-[40px]" />}
    >
      <GcpCloudStorageConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
