import { PageBody, PageHeader } from '@/components/ds';
import { getTranslations } from 'next-intl/server';
import { ReactElement } from 'react';
import NewApiKeyForm from './NewApiKeyForm';

export default async function NewAccountApiKey(): Promise<ReactElement> {
  const t = await getTranslations('settings.apiKeys.new');
  return (
    <PageBody className="max-w-[1320px]">
      <PageHeader title={t('title')} description={t('description')} />
      <NewApiKeyForm />
    </PageBody>
  );
}
