import { Badge } from '@/components/ui/badge';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Skeleton } from '@/components/ui/skeleton';
import { cn } from '@/libs/utils';
import { useMutation } from '@connectrpc/connect-query';
import { ColumnSampleValue, ConnectionDataService } from '@husonym/sdk';
import { ReactElement, useEffect, useState } from 'react';

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
  connectionId: string;
  schema: string;
  table: string;
  column: string;
  dataType?: string;
  // Nombre de valeurs demandées.
  limit?: number;
}

// Nombre de lignes affichées par défaut : assez pour juger une colonne d'un coup
// d'œil sans transformer la fenêtre en explorateur de données.
const DEFAULT_LIMIT = 20;

// Aperçu des premières valeurs d'une colonne.
//
// Sert la levée de doute : face à un badge « à vérifier », regarder les données
// réelles est le moyen le plus rapide de trancher. La fenêtre est délibérément
// en lecture seule.
export default function ColumnPreviewDialog({
  open,
  onOpenChange,
  connectionId,
  schema,
  table,
  column,
  dataType,
  limit = DEFAULT_LIMIT,
}: Props): ReactElement {
  const [values, setValues] = useState<ColumnSampleValue[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { mutateAsync: getSample } = useMutation(
    ConnectionDataService.method.getColumnSampleValues
  );

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setValues(null);
    setError(null);
    getSample({ connectionId, schema, table, column, limit })
      .then((resp) => {
        if (!cancelled) {
          setValues(resp.values);
        }
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : 'erreur inconnue');
        }
      });
    // Une fenêtre déjà fermée ne doit pas écrire dans un état démonté.
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, connectionId, schema, table, column, limit]);

  const filled = values?.filter((v) => !v.isNull && v.value !== '').length ?? 0;
  const nulls = values?.filter((v) => v.isNull).length ?? 0;
  const empties = values?.filter((v) => !v.isNull && v.value === '').length ?? 0;
  const distinct = new Set(
    values?.filter((v) => !v.isNull).map((v) => v.value) ?? []
  ).size;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span className="font-mono text-base">{column}</span>
            {dataType && (
              <Badge variant="outline" className="font-mono text-xs font-normal">
                {dataType}
              </Badge>
            )}
          </DialogTitle>
          <DialogDescription className="font-mono text-xs">
            {schema}.{table}
          </DialogDescription>
        </DialogHeader>

        {error && (
          <p className="rounded-md border border-red-600/40 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-500/40 dark:bg-red-950/40 dark:text-red-400">
            Lecture impossible : {error}
          </p>
        )}

        {!error && values === null && (
          <div className="flex flex-col gap-2 py-2">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-7 w-full" />
            ))}
          </div>
        )}

        {!error && values !== null && (
          <>
            {/* Compteurs : ils portent souvent la réponse à eux seuls (une colonne
                entièrement nulle, ou à valeur unique, se juge sans lire le détail). */}
            <div className="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs">
              <span>
                <strong className="text-foreground">{values.length}</strong>{' '}
                ligne(s)
              </span>
              <span>
                <strong className="text-foreground">{distinct}</strong> valeur(s)
                distincte(s)
              </span>
              {nulls > 0 && <span>{nulls} NULL</span>}
              {empties > 0 && <span>{empties} vide(s)</span>}
              {filled === 0 && values.length > 0 && (
                <span className="text-amber-600 dark:text-amber-400">
                  aucune valeur exploitable
                </span>
              )}
            </div>

            <div className="max-h-[55vh] overflow-auto rounded-md border">
              <table className="w-full text-sm">
                <thead className="bg-muted/50 sticky top-0">
                  <tr>
                    <th className="text-muted-foreground w-12 px-3 py-2 text-right text-xs font-medium">
                      #
                    </th>
                    <th className="text-muted-foreground px-3 py-2 text-left text-xs font-medium">
                      Valeur
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {values.length === 0 && (
                    <tr>
                      <td
                        colSpan={2}
                        className="text-muted-foreground px-3 py-6 text-center text-sm"
                      >
                        La table ne contient aucune ligne.
                      </td>
                    </tr>
                  )}
                  {values.map((v, i) => (
                    <tr
                      key={i}
                      className={cn(
                        'border-t',
                        i % 2 === 1 && 'bg-muted/20',
                        'hover:bg-muted/40'
                      )}
                    >
                      <td className="text-muted-foreground px-3 py-1.5 text-right font-mono text-xs tabular-nums">
                        {i + 1}
                      </td>
                      <td className="px-3 py-1.5 font-mono text-xs break-all">
                        {v.isNull ? (
                          <span className="text-muted-foreground/60 italic">
                            NULL
                          </span>
                        ) : v.value === '' ? (
                          <span className="text-muted-foreground/60 italic">
                            (chaîne vide)
                          </span>
                        ) : (
                          v.value
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="text-muted-foreground text-xs">
              Échantillon en lecture seule des {limit} premières lignes.
            </p>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
