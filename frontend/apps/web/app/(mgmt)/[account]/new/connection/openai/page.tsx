'use client';
import OpenAiConnectionForm from '@/components/connections/forms/openai/OpenAiConnectionForm';
import NewConnectionLayout from '../components/NewConnectionLayout';
import { useTheme } from 'next-themes';
import { ReactElement, useMemo } from 'react';
import { useOnCreateSuccess } from '../components/useOnCreateSuccess';
import { OpenAiLogo } from './OpenAiLogo';

export default function OpenAi(): ReactElement {
  const { resolvedTheme } = useTheme();
  const logoBg = useMemo(
    () => (resolvedTheme === 'dark' ? 'white' : '#272F30'),
    [resolvedTheme]
  );

  const onSuccess = useOnCreateSuccess();
  return (
    <NewConnectionLayout
      header="OpenAI"
      subHeadings="Configure an OpenAI SDK to be used for AI-focused data generation jobs."
      leftIcon={<OpenAiLogo bg={logoBg} />}
    >
      <OpenAiConnectionForm mode="create" onSuccess={onSuccess} />
    </NewConnectionLayout>
  );
}
