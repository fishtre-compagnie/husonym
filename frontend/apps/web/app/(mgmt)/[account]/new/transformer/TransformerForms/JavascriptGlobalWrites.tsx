'use client';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';

// Avertit quand une règle écrit hors de ses variables : un script qui comptait garder
// une valeur d'une ligne à la suivante (un pseudonyme déjà tiré, par exemple) ne la
// retrouve plus, et risque de rendre la valeur source.
interface Props {
  globalWrites: string[];
}

export default function JavascriptGlobalWrites(
  props: Props
): ReactElement | null {
  const { globalWrites } = props;
  const t = useTranslations('connections.newTransformer.code');
  if (globalWrites.length === 0) {
    return null;
  }
  return (
    <Alert variant="warning" className="mt-4">
      <AlertTitle>{t('globalWritesTitle')}</AlertTitle>
      <AlertDescription>
        <p>
          <code>{globalWrites.join(', ')}</code>
        </p>
        <p className="mt-1">{t('globalWritesDescription')}</p>
      </AlertDescription>
    </Alert>
  );
}
