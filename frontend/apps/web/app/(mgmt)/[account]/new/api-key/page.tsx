import { PageBody, PageHeader } from '@/components/ds';
import { ReactElement } from 'react';
import NewApiKeyForm from './NewApiKeyForm';

export default function NewAccountApiKey(): ReactElement {
  return (
    <PageBody className="max-w-[1320px]">
      <PageHeader
        title="New API key"
        description="API Keys can be used to interact with Husonym programmatically"
      />
      <NewApiKeyForm />
    </PageBody>
  );
}
