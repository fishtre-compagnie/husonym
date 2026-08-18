import EditTransformerOptions from '@/app/(mgmt)/[account]/transformers/EditTransformerOptions';
import TruncatedText from '@/components/TruncatedText';
import { Badge } from '@/components/ui/badge';
import {
  getTransformerSelectButtonText,
  isInvalidTransformer,
} from '@/util/util';
import { JobMappingTransformerForm } from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import {
  PiiConfidence,
  PiiDetectionMethod,
  SystemTransformerSchema,
  TransformerSource,
} from '@husonym/sdk';
import { ColumnDef, createColumnHelper, Row } from '@tanstack/react-table';
import ColumnPreviewButton from './ColumnPreviewButton';
import RgpdCell from './RgpdCell';
import { DataTableRowActions } from '../NosqlTable/data-table-row-actions';
import EditCollection from '../NosqlTable/EditCollection';
import EditDocumentKey from '../NosqlTable/EditDocumentKey';
import { SchemaColumnHeader } from '../SchemaTable/SchemaColumnHeader';
import TransformerSelect from '../SchemaTable/TransformerSelect';
import ConstraintsCell from './ConstraintsCell';
import DataTypeCell from './DataTypeCell';
import IndeterminateCheckbox from './IndeterminateCheckbox';

export interface JobMappingRow {
  schema: string;
  table: string;
  column: string;
  constraints: RowConstraint;
  dataType: string;
  isNullable: boolean;
  attributes: RowAttribute;
  transformer: JobMappingTransformerForm;
  // Détection RGPD (heuristique backend, cf. pkg/piidetect)
  isSensitive: boolean;
  dataCategory?: string;
  suggestedTransformerSource: TransformerSource;
  // Niveau de confiance de la détection : décide de la couleur du badge (vert
  // confirmé / orange à vérifier) et si le transformer a pu être appliqué seul.
  piiConfidence?: PiiConfidence;
  piiDetectionMethod?: PiiDetectionMethod;
  piiEvidence?: string;
}

interface RowAttribute {
  value: string; // accessor fn value for search

  generatedType: string | undefined;
  identityType: string | undefined;
}
interface RowConstraint {
  value: string; // accessor fn value for search
  isPrimaryKey: boolean;
  foreignKey: [boolean, string[]];
  virtualForeignKey: [boolean, string[]];
  isUnique: boolean;
}

export interface NosqlJobMappingRow {
  collection: string; // combined schema.table
  column: string;
  transformer: JobMappingTransformerForm;
}

// Un transformer anonymise-t-il réellement la valeur ? « Passthrough » (ou aucun
// choix) recopie la donnée d'origine vers la destination : une colonne
// personnelle laissée ainsi part en clair.
function isAnonymizingTransformer(t: JobMappingTransformerForm): boolean {
  const c = t?.config?.case;
  return !!c && c !== 'passthroughConfig';
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function getJobMappingColumns(): ColumnDef<JobMappingRow, any>[] {
  const columnHelper = createColumnHelper<JobMappingRow>();

  const checkboxColumn = columnHelper.display({
    id: 'isSelected',
    header({ table }) {
      return (
        <IndeterminateCheckbox
          {...{
            checked: table.getIsAllRowsSelected(),
            indeterminate: table.getIsSomeRowsSelected(),
            onChange: table.getToggleAllRowsSelectedHandler(),
          }}
        />
      );
    },
    cell({ row }) {
      return (
        <div>
          <IndeterminateCheckbox
            {...{
              checked: row.getIsSelected(),
              indeterminate: row.getIsSomeSelected(),
              onChange: row.getToggleSelectedHandler(),
            }}
          />
        </div>
      );
    },
    maxSize: 20,
  });

  const schemaColumn = columnHelper.accessor('schema', {
    size: 105,
    header({ column }) {
      return <SchemaColumnHeader column={column} title="Schema" />;
    },
    cell({ getValue }) {
      return <TruncatedText text={getValue()} />;
    },
  });

  const tableColumn = columnHelper.accessor((row) => row.table, {
    id: 'table',
    size: 120,
    header({ column }) {
      return <SchemaColumnHeader column={column} title="Table" />;
    },
    cell({ getValue }) {
      return <TruncatedText text={getValue()} maxWidth={150} />;
    },
  });

  const columnColumn = columnHelper.accessor('column', {
    size: 180,
    header({ column }) {
      return <SchemaColumnHeader column={column} title="Column" />;
    },
    cell({ getValue }) {
      return <TruncatedText text={getValue()} maxWidth={150} />;
    },
  });

  const dataTypeColumn = columnHelper.accessor('dataType', {
    size: 122,
    header({ column }) {
      return <SchemaColumnHeader column={column} title="Data Type" />;
    },
    cell({ getValue }) {
      return <DataTypeCell value={getValue()} />;
    },
  });

  const isNullableColumn = columnHelper.accessor(
    (row) => (row.isNullable ? 'Yes' : 'No') as string,
    {
      id: 'isNullable',
      size: 115,
      header({ column }) {
        return <SchemaColumnHeader column={column} title="Nullable" />;
      },
      cell({ getValue }) {
        return (
          <span className="max-w-[500px] truncate font-medium">
            <Badge variant="outline">{getValue()}</Badge>
          </span>
        );
      },
    }
  );

  const constraintColumn = columnHelper.accessor(
    (row) => row.constraints.value,
    {
      id: 'constraints',
      size: 128,
      header({ column }) {
        return <SchemaColumnHeader column={column} title="Constraints" />;
      },
      cell({ row }) {
        const constraints = row.original.constraints;
        return (
          <ConstraintsCell
            isPrimaryKey={constraints.isPrimaryKey}
            foreignKey={constraints.foreignKey}
            virtualForeignKey={constraints.virtualForeignKey}
            isUnique={constraints.isUnique}
          />
        );
      },
    }
  );

  const transformerColumn = columnHelper.accessor(
    (row) => {
      if (row.transformer.config.case) {
        // this needs to be the full transformer object so that memoization works correctly
        return row.transformer;
      }
      return 'transformer';
    },
    {
      id: 'transformer',
      size: 244,
      header({ column }) {
        return <SchemaColumnHeader column={column} title="Transformer" />;
      },
      cell({ table, row }) {
        const transformer =
          table.options.meta?.jmTable?.getTransformerFromField(row.index) ??
          create(SystemTransformerSchema);
        const transformerForm = row.original.transformer;
        return (
          <div className="flex flex-row gap-2">
            <div>
              <TransformerSelect
                getTransformers={() =>
                  table.options.meta?.jmTable?.getAvailableTransformers(
                    row.index
                  ) ?? {
                    system: [],
                    userDefined: [],
                  }
                }
                buttonText={getTransformerSelectButtonText(transformer)}
                buttonClassName="w-[195px]"
                value={transformerForm}
                onSelect={(updatedValue) =>
                  table.options.meta?.jmTable?.onTransformerUpdate(
                    row.index,
                    updatedValue
                  )
                }
                disabled={false}
              />
            </div>
            <div>
              <EditTransformerOptions
                transformer={transformer}
                value={transformerForm}
                onSubmit={(updatedValue) => {
                  table.options.meta?.jmTable?.onTransformerUpdate(
                    row.index,
                    updatedValue
                  );
                }}
                disabled={isInvalidTransformer(transformer)}
              />
            </div>
          </div>
        );
      },
      filterFn: transformerFilterFn,
      sortingFn: transformerSortingFn,
    }
  );

  // accessor et NON display : une colonne `display` n'a pas de valeur dérivée des
  // données, donc TanStack réutilise sa cellule mémoïsée. Résultat observé : après
  // un scan de contenu, le transformer se mettait bien à jour (colonne accessor)
  // alors que le badge RGPD restait figé. La chaîne renvoyée ici change dès que la
  // détection change, ce qui force le rendu — même raison que le commentaire de la
  // colonne Transformer ci-dessous.
  // La valeur commence par un rang pour que le tri soit utile : trier en
  // décroissant remonte les colonnes « à vérifier », celles qui demandent une
  // décision. Le reste de la chaîne garantit le rafraîchissement de la cellule.
  const rgpdColumn = columnHelper.accessor(
    (row) => {
      const anonymise = isAnonymizingTransformer(row.transformer);
      // Rang décroissant = gravité décroissante au tri inverse :
      //   3 non traité (part en clair) > 2 à vérifier > 1 conforme > 0 rien.
      const rank =
        row.isSensitive &&
        row.piiConfidence !== PiiConfidence.NEEDS_REVIEW &&
        !anonymise
          ? 3
          : row.piiConfidence === PiiConfidence.NEEDS_REVIEW
            ? 2
            : row.isSensitive
              ? 1
              : 0;
      // `anonymise` fait partie de la clé : sans lui, changer le transformer ne
      // rafraîchirait pas le badge (cellule mémoïsée par TanStack).
      return `${rank}|${anonymise}|${row.piiConfidence ?? 0}|${row.piiDetectionMethod ?? 0}|${row.dataCategory ?? ''}`;
    },
    {
      id: 'rgpd',
      size: 112,
      header({ column }) {
        return <SchemaColumnHeader column={column} title="RGPD" />;
      },
      cell({ row }) {
        return (
          // justify-start : centré, le badge ne tombait pas sous le libellé
          // « RGPD » de l'en-tête, aligné à gauche comme toutes les colonnes.
          <div className="flex justify-start">
            <RgpdCell
              isSensitive={row.original.isSensitive}
              dataCategory={row.original.dataCategory}
              confidence={row.original.piiConfidence}
              method={row.original.piiDetectionMethod}
              isAnonymized={isAnonymizingTransformer(row.original.transformer)}
              hasSuggestion={
                row.original.suggestedTransformerSource !==
                TransformerSource.UNSPECIFIED
              }
            />
          </div>
        );
      },
    }
  );

  const previewColumn = columnHelper.display({
    id: 'preview',
    size: 44,
    header() {
      return <span className="sr-only">Aperçu des données</span>;
    },
    cell({ row, table }) {
      const connectionId = table.options.meta?.jmTable?.sourceConnectionId;
      // Job generate : aucune donnée source à lire, le bouton n'aurait rien à montrer.
      if (!connectionId) {
        return null;
      }
      return (
        <ColumnPreviewButton
          connectionId={connectionId}
          schema={row.original.schema}
          table={row.original.table}
          column={row.original.column}
          dataType={row.original.dataType}
        />
      );
    },
  });

  return [
    checkboxColumn,
    schemaColumn,
    tableColumn,
    columnColumn,
    dataTypeColumn,
    isNullableColumn,
    constraintColumn,
    rgpdColumn,
    previewColumn,
    transformerColumn,
  ];
}

function transformerSortingFn(
  rowA: Row<JobMappingRow>,
  rowB: Row<JobMappingRow>,
  _columnId: string
): number {
  return rowA.original.transformer.config.case.localeCompare(
    rowB.original.transformer.config.case
  );
}

function transformerFilterFn(
  row: Row<JobMappingRow>,
  columnId: string,
  fitlerValue: any // eslint-disable-line @typescript-eslint/no-explicit-any
): boolean;
function transformerFilterFn(
  row: Row<NosqlJobMappingRow>,
  columnId: string,
  fitlerValue: any // eslint-disable-line @typescript-eslint/no-explicit-any
): boolean;
function transformerFilterFn(
  row: Row<JobMappingRow | NosqlJobMappingRow>,
  columnId: string,
  filterValue: any // eslint-disable-line @typescript-eslint/no-explicit-any
): boolean {
  const value = row.getValue<JobMappingTransformerForm | string>(columnId);
  const loweredFilterValue = filterValue.toLowerCase();
  if (typeof value === 'string') {
    return value.includes(loweredFilterValue);
  }
  const searchableFields = [value?.config.case].filter(Boolean);
  return searchableFields.some((field) =>
    field.toLowerCase().includes(loweredFilterValue)
  );
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function getNosqlJobMappingColumns(): ColumnDef<NosqlJobMappingRow, any>[] {
  const columnHelper = createColumnHelper<NosqlJobMappingRow>();

  const checkboxColumn = columnHelper.display({
    id: 'isSelected',
    header({ table }) {
      return (
        <IndeterminateCheckbox
          {...{
            checked: table.getIsAllRowsSelected(),
            indeterminate: table.getIsSomeRowsSelected(),
            onChange: table.getToggleAllRowsSelectedHandler(),
          }}
        />
      );
    },
    cell({ row }) {
      return (
        <div>
          <IndeterminateCheckbox
            {...{
              checked: row.getIsSelected(),
              indeterminate: row.getIsSomeSelected(),
              onChange: row.getToggleSelectedHandler(),
            }}
          />
        </div>
      );
    },
  });

  const collectionColumn = columnHelper.accessor('collection', {
    header({ column }) {
      return <SchemaColumnHeader column={column} title="Collection" />;
    },
    cell({ getValue, table, row }) {
      return (
        <EditCollection
          text={getValue()}
          collections={
            table.options.meta?.jmTable?.getAvailableCollectionsByRow(
              row.index
            ) ?? []
          }
          onEdit={(updatedValue) => {
            if (table.options.meta?.jmTable?.onRowUpdate) {
              table.options.meta.jmTable.onRowUpdate(row.index, {
                ...row.original,
                collection: updatedValue.collection,
              });
            }
          }}
        />
      );
    },
  });

  const columnColumn = columnHelper.accessor('column', {
    header({ column }) {
      return <SchemaColumnHeader column={column} title="Document Key" />;
    },
    cell({ getValue, table, row }) {
      return (
        <EditDocumentKey
          text={getValue()}
          isDuplicate={(newValue, currValue) => {
            return (
              newValue !== currValue &&
              (table.options.meta?.jmTable?.canRenameColumn(
                row.index,
                newValue
              ) ??
                false)
            );
          }}
          onEdit={(updatedValue) => {
            if (table.options.meta?.jmTable?.onRowUpdate) {
              table.options.meta.jmTable.onRowUpdate(row.index, {
                ...row.original,
                column: updatedValue.column,
              });
            }
          }}
        />
      );
    },
  });

  const transformerColumn = columnHelper.accessor(
    (row) => {
      if (row.transformer.config.case) {
        // this needs to be the full transformer object so that memoization works correctly
        return row.transformer;
      }
      return 'transformer';
    },
    {
      id: 'transformer',
      header({ column }) {
        return <SchemaColumnHeader column={column} title="Transformer" />;
      },
      cell({ table, row }) {
        const transformer =
          table.options.meta?.jmTable?.getTransformerFromField(row.index) ??
          create(SystemTransformerSchema);
        const transformerForm = row.original.transformer;
        return (
          <div className="flex flex-row gap-2">
            <div>
              <TransformerSelect
                getTransformers={() =>
                  table.options.meta?.jmTable?.getAvailableTransformers(
                    row.index
                  ) ?? {
                    system: [],
                    userDefined: [],
                  }
                }
                buttonText={getTransformerSelectButtonText(transformer)}
                buttonClassName="w-[175px]"
                value={transformerForm}
                onSelect={(updatedValue) =>
                  table.options.meta?.jmTable?.onTransformerUpdate(
                    row.index,
                    updatedValue
                  )
                }
                disabled={false}
              />
            </div>
            <div>
              <EditTransformerOptions
                transformer={transformer}
                value={transformerForm}
                onSubmit={(updatedValue) => {
                  table.options.meta?.jmTable?.onTransformerUpdate(
                    row.index,
                    updatedValue
                  );
                }}
                disabled={isInvalidTransformer(transformer)}
              />
            </div>
          </div>
        );
      },
      filterFn: transformerFilterFn,
    }
  );

  const actionsColumn = columnHelper.display({
    id: 'actions',
    header({}) {
      return <p>Actions</p>;
    },
    cell({ row, table }) {
      return (
        <DataTableRowActions
          row={row}
          onDuplicate={() =>
            table.options.meta?.jmTable?.onDuplicateRow(row.index)
          }
          onDelete={() => table.options.meta?.jmTable?.onDeleteRow(row.index)}
        />
      );
    },
  });

  return [
    checkboxColumn,
    collectionColumn,
    columnColumn,
    transformerColumn,
    actionsColumn,
  ];
}

export const SQL_COLUMNS = getJobMappingColumns();

export const NOSQL_COLUMNS = getNosqlJobMappingColumns();
