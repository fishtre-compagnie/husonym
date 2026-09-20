'use client';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { ReactElement } from 'react';

interface Props {
  globalWrites: string[];
}

// Avertit quand une règle écrit hors de ses variables : un script qui comptait garder
// une valeur d'une ligne à la suivante (un pseudonyme déjà tiré, par exemple) ne la
// retrouve plus, et risque de rendre la valeur source.
export default function JavascriptGlobalWrites(
  props: Props
): ReactElement | null {
  const { globalWrites } = props;
  if (globalWrites.length === 0) {
    return null;
  }
  return (
    <Alert variant="warning" className="mt-4">
      <AlertTitle>Ce code écrit hors de ses variables</AlertTitle>
      <AlertDescription>
        <p>
          <code>{globalWrites.join(', ')}</code>
        </p>
        <p className="mt-1">
          Cet état ne vit que le temps d’une ligne : il est partagé avec les
          autres colonnes de la ligne, et perdu à la suivante. Pour obtenir la
          même sortie pour la même valeur, utilisez les fonctions pseudo.*
          (Athanor).
        </p>
      </AlertDescription>
    </Alert>
  );
}
