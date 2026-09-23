'use client';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
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
  if (globalWrites.length === 0) {
    return null;
  }
  return (
    <Alert variant="warning" className="mt-4">
      <AlertTitle>This code writes outside its own variables</AlertTitle>
      <AlertDescription>
        <p>
          <code>{globalWrites.join(', ')}</code>
        </p>
        <p className="mt-1">
          That state only lives for one row: it is shared with the other columns
          of the row, and lost at the next one. To turn the same value into the
          same output, use the pseudo.* functions (Athanor).
        </p>
      </AlertDescription>
    </Alert>
  );
}
