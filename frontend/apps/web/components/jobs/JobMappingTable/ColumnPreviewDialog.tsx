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
import { toJsonString } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import {
  ColumnSampleValue,
  ConnectionDataService,
  TransformerConfig,
  TransformerConfigSchema,
} from '@husonym/sdk';
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
  // Transformer à essayer sur ces valeurs. Sans lui, la fenêtre montre la colonne telle quelle ;
  // avec lui, chaque valeur à côté de ce que le transformer en fait.
  transformer?: TransformerConfig;
  transformerName?: string;
}

// Une ligne de l'aperçu : la valeur lue, et ce qu'en fait le transformer quand il y en a un.
interface PreviewRow {
  input: ColumnSampleValue;
  output?: ColumnSampleValue;
  error?: string;
}

// Nombre de lignes affichées par défaut : assez pour juger une colonne d'un coup
// d'œil sans transformer la fenêtre en explorateur de données.
const DEFAULT_LIMIT = 20;

// Aperçu des premières valeurs d'une colonne, avant et après un transformer.
//
// Sert la levée de doute : face à un badge « à vérifier », regarder les données
// réelles est le moyen le plus rapide de trancher. Avec un transformer, il sert aussi
// à voir ce qu'il produira avant de s'y engager — et surtout s'il ramène des valeurs
// différentes à la même, ce qu'une colonne unique refuserait. La fenêtre est
// délibérément en lecture seule.
export default function ColumnPreviewDialog({
  open,
  onOpenChange,
  connectionId,
  schema,
  table,
  column,
  dataType,
  limit = DEFAULT_LIMIT,
  transformer,
  transformerName,
}: Props): ReactElement {
  const [rows, setRows] = useState<PreviewRow[] | null>(null);
  const [distinct, setDistinct] = useState<{
    inputs: number;
    outputs?: number;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { mutateAsync: getSample } = useMutation(
    ConnectionDataService.method.getColumnSampleValues
  );
  const { mutateAsync: previewTransformer } = useMutation(
    ConnectionDataService.method.previewColumnTransformer
  );

  // Clé stable du transformer : l'objet change d'identité à chaque rendu, sa
  // sérialisation non — sans elle, l'effet relancerait la lecture en boucle.
  const transformerKey = transformer
    ? toJsonString(TransformerConfigSchema, transformer)
    : '';

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    setRows(null);
    setDistinct(null);
    setError(null);

    const load = transformer
      ? previewTransformer({
          connectionId,
          schema,
          table,
          column,
          transformer,
          limit,
        }).then((resp) => ({
          rows: resp.values.map((v): PreviewRow => ({
            input: v.input!,
            output: v.output,
            error: v.error,
          })),
          distinct: {
            inputs: resp.distinctInputs,
            outputs: resp.distinctOutputs,
          },
        }))
      : getSample({ connectionId, schema, table, column, limit }).then(
          (resp) => ({
            rows: resp.values.map((v): PreviewRow => ({ input: v })),
            distinct: {
              inputs: new Set(
                resp.values.filter((v) => !v.isNull).map((v) => v.value)
              ).size,
            },
          })
        );

    load
      .then((result) => {
        if (!cancelled) {
          setRows(result.rows);
          setDistinct(result.distinct);
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
  }, [open, connectionId, schema, table, column, limit, transformerKey]);

  const inputs = rows?.map((r) => r.input) ?? [];
  const filled = inputs.filter((v) => !v.isNull && v.value !== '').length;
  const nulls = inputs.filter((v) => v.isNull).length;
  const empties = inputs.filter((v) => !v.isNull && v.value === '').length;
  const failures = rows?.filter((r) => r.error).length ?? 0;
  // Une valeur en échec ne produit rien, ce qui fait baisser le compte d'après sans
  // que le transformer ait rien confondu : l'alerte ne vaut que si tout a abouti.
  const collapses =
    failures === 0 &&
    distinct?.outputs !== undefined &&
    distinct.outputs < distinct.inputs;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={transformer ? 'max-w-4xl' : 'max-w-2xl'}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span className="font-mono text-base">{column}</span>
            {dataType && (
              <Badge
                variant="outline"
                className="font-mono text-xs font-normal"
              >
                {dataType}
              </Badge>
            )}
          </DialogTitle>
          <DialogDescription className="font-mono text-xs">
            {schema}.{table}
            {transformer && (
              <span className="font-sans">
                {' '}
                — aperçu avec {transformerName ?? 'le transformer choisi'}
              </span>
            )}
          </DialogDescription>
        </DialogHeader>

        {error && (
          <p className="rounded-md border border-red-600/40 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-500/40 dark:bg-red-950/40 dark:text-red-400">
            Lecture impossible : {error}
          </p>
        )}

        {!error && rows === null && (
          <div className="flex flex-col gap-2 py-2">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="h-7 w-full" />
            ))}
          </div>
        )}

        {!error && rows !== null && (
          <>
            {/* Compteurs : ils portent souvent la réponse à eux seuls (une colonne
                entièrement nulle, ou à valeur unique, se juge sans lire le détail). */}
            <div className="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs">
              <span>
                <strong className="text-foreground">{rows.length}</strong>{' '}
                ligne(s)
              </span>
              <span>
                <strong className="text-foreground">{distinct?.inputs}</strong>{' '}
                valeur(s) distincte(s)
                {distinct?.outputs !== undefined && (
                  <>
                    {' '}
                    avant,{' '}
                    <strong className="text-foreground">
                      {distinct.outputs}
                    </strong>{' '}
                    après
                  </>
                )}
              </span>
              {nulls > 0 && <span>{nulls} NULL</span>}
              {empties > 0 && <span>{empties} vide(s)</span>}
              {failures > 0 && (
                <span className="text-red-600 dark:text-red-400">
                  {failures} en échec
                </span>
              )}
              {filled === 0 && rows.length > 0 && (
                <span className="text-amber-600 dark:text-amber-400">
                  aucune valeur exploitable
                </span>
              )}
            </div>

            {collapses && (
              // Le seul signal qui vaille avant un run : des valeurs différentes
              // deviennent la même. Sur une colonne unique, l'écriture échouerait.
              <p className="rounded-md border border-amber-600/40 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-500/40 dark:bg-amber-950/40 dark:text-amber-300">
                Le transformer ramène {distinct?.inputs} valeurs distinctes à{' '}
                {distinct?.outputs} : sur une colonne soumise à une contrainte
                d&apos;unicité, l&apos;écriture échouerait.
              </p>
            )}

            <div className="max-h-[55vh] overflow-auto rounded-md border">
              <table className="w-full text-sm">
                <thead className="bg-muted/50 sticky top-0">
                  <tr>
                    <th className="text-muted-foreground w-12 px-3 py-2 text-right text-xs font-medium">
                      #
                    </th>
                    <th className="text-muted-foreground px-3 py-2 text-left text-xs font-medium">
                      {transformer ? 'Avant' : 'Valeur'}
                    </th>
                    {transformer && (
                      <th className="text-muted-foreground px-3 py-2 text-left text-xs font-medium">
                        Après
                      </th>
                    )}
                  </tr>
                </thead>
                <tbody>
                  {rows.length === 0 && (
                    <tr>
                      <td
                        colSpan={transformer ? 3 : 2}
                        className="text-muted-foreground px-3 py-6 text-center text-sm"
                      >
                        La table ne contient aucune ligne.
                      </td>
                    </tr>
                  )}
                  {rows.map((r, i) => (
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
                        <SampleValue value={r.input} />
                      </td>
                      {transformer && (
                        <td className="px-3 py-1.5 font-mono text-xs break-all">
                          {r.error ? (
                            <span className="text-red-600 dark:text-red-400">
                              {r.error}
                            </span>
                          ) : r.output ? (
                            <SampleValue value={r.output} />
                          ) : null}
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <p className="text-muted-foreground text-xs">
              Échantillon en lecture seule des {limit} premières lignes
              {transformer &&
                ' : il montre ce que fait le transformer, il ne prouve rien sur la table entière'}
              .
            </p>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

function SampleValue({ value }: { value: ColumnSampleValue }): ReactElement {
  if (value.isNull) {
    return <span className="text-muted-foreground/60 italic">NULL</span>;
  }
  if (value.value === '') {
    return (
      <span className="text-muted-foreground/60 italic">(chaîne vide)</span>
    );
  }
  return <>{value.value}</>;
}
