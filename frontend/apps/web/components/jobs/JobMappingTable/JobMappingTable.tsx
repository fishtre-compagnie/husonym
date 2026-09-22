import FastTable from '@/components/FastTable/FastTable';
import {
  AppTableFeatures,
  unpaginatedTableFeatures,
} from '@/components/table/features';
import { CardDescription, CardTitle } from '@/components/ui/card';
import { Transformer } from '@/shared/transformers';
import { JobMappingTransformerForm } from '@/yup-validations/jobs';
import { JobMapping } from '@husonym/sdk';
import {
  ColumnDef,
  Row,
  RowData,
  TableFeatures,
  useTable,
} from '@tanstack/react-table';
import { ReactElement, useCallback, useState } from 'react';
import { GoWorkflow } from 'react-icons/go';
import { ImportMappingsConfig } from '../SchemaTable/ImportJobMappingsButton';
import { SchemaTableToolbar } from '../SchemaTable/SchemaTableToolBar';
import { TransformerResult } from '../SchemaTable/transformer-handler';
import MappingDecisionPanel, {
  ColumnDecisionTarget,
} from './MappingDecisionPanel';
import SelectionBar from './SelectionBar';

interface Props<TData extends RowData> {
  data: TData[];
  columns: ColumnDef<AppTableFeatures, TData>[];
  onTransformerUpdate(index: number, config: JobMappingTransformerForm): void;
  getAvailableTransformers(index: number): TransformerResult;
  getTransformerFromField(index: number): Transformer;

  onTransformerBulkUpdate(
    indices: number[],
    config: JobMappingTransformerForm
  ): void;
  getAvalableTransformersForBulk(
    rows: Row<AppTableFeatures, TData>[]
  ): TransformerResult;
  getTransformerFromFieldValue(value: JobMappingTransformerForm): Transformer;

  isApplyDefaultTransformerButtonDisabled: boolean;
  displayApplyDefaultTransformersButton: boolean;
  onApplyDefaultClick(override: boolean): void;

  onExportMappingsClick(
    selected: Row<AppTableFeatures, TData>[],
    shouldFormat: boolean
  ): void;
  onImportMappingsClick(
    jobmappings: JobMapping[],
    config: ImportMappingsConfig
  ): void;

  onDuplicateRow(index: number): void;
  onDeleteRow(index: number): void;
  canRenameColumn(index: number, newColumn: string): boolean;
  onRowUpdate(index: number, newValue: TData): void;
  getAvailableCollectionsByRow(index: number): string[];
  hasMissingSourceColumnMappings: boolean;
  onRemoveMissingSourceColumnMappings(): void;

  // Id de la connexion source : l'aperçu du panneau y lit la colonne.
  sourceConnectionId?: string;

  // La colonne d'une ligne, telle que le panneau de décision l'attend. Absente —
  // les tables NoSQL, qui n'ont ni type ni contraintes — aucun panneau ne s'ouvre.
  getColumnDecision?(index: number): ColumnDecisionTarget | undefined;

  // Scan de contenu PII (Presidio) — actif uniquement pour les jobs sync.
  showPiiScan?: boolean;
  onScanContent?(): void;
  isScanningPii?: boolean;
}

declare module '@tanstack/react-table' {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface TableMeta<TFeatures extends TableFeatures, TData extends RowData> {
    jmTable?: {
      onTransformerUpdate(
        rowIndex: number,
        transformer: JobMappingTransformerForm
      ): void;
      getAvailableTransformers(rowIndex: number): TransformerResult;
      getTransformerFromField(index: number): Transformer;

      onDuplicateRow(rowIndex: number): void;
      onDeleteRow(rowIndex: number): void;
      canRenameColumn(rowIndex: number, newColumn: string): boolean;
      onRowUpdate(rowIndex: number, newValue: TData): void;
      // Returns the available schema.table list
      getAvailableCollectionsByRow(rowIndex: number): string[];
      // Id de la connexion source. Absent pour les jobs generate : il n'y a
      // alors aucune donnée à échantillonner, le panneau se passe d'aperçu.
      sourceConnectionId?: string;
      // Ouvre la décision de la ligne dans le panneau latéral.
      onOpenDecision?(rowIndex: number): void;
    };
  }
}

export default function JobMappingTable<TData extends RowData>(
  props: Props<TData>
): ReactElement {
  const {
    data,
    columns,
    onTransformerUpdate,
    getAvailableTransformers,
    getTransformerFromField,
    onExportMappingsClick,
    onImportMappingsClick,
    getAvalableTransformersForBulk,
    getTransformerFromFieldValue,
    isApplyDefaultTransformerButtonDisabled,
    displayApplyDefaultTransformersButton,
    onApplyDefaultClick,
    onTransformerBulkUpdate,
    onDeleteRow,
    onDuplicateRow,
    canRenameColumn,
    onRowUpdate,
    getAvailableCollectionsByRow,
    hasMissingSourceColumnMappings,
    onRemoveMissingSourceColumnMappings,
    sourceConnectionId,
    getColumnDecision,
    showPiiScan,
    onScanContent,
    isScanningPii,
  } = props;

  // La ligne dont la décision est ouverte. Stable d'un rendu à l'autre : les lignes
  // sont mémoïsées et ne reliraient pas une nouvelle fonction.
  const [opened, setOpened] = useState<number | null>(null);
  const onOpenDecision = useCallback((index: number) => setOpened(index), []);
  const closeDecision = useCallback(() => setOpened(null), []);

  const table = useTable({
    features: unpaginatedTableFeatures,
    data,
    columns,
    meta: {
      jmTable: {
        onTransformerUpdate,
        getAvailableTransformers,
        getTransformerFromField,
        onDeleteRow,
        onDuplicateRow,
        canRenameColumn,
        onRowUpdate,
        getAvailableCollectionsByRow,
        sourceConnectionId,
        onOpenDecision: getColumnDecision ? onOpenDecision : undefined,
      },
    },
  });

  // La navigation du panneau suit l'ordre affiché, filtres et tri compris, et non
  // l'ordre des données.
  const visible = table.getRowModel().rows;
  const selectedRows = table.getSelectedRowModel().rows;
  const position =
    opened === null ? -1 : visible.findIndex((r) => r.index === opened);
  const target = opened === null ? undefined : getColumnDecision?.(opened);

  return (
    <div>
      <div className="flex flex-row items-center gap-2 pt-4 ">
        <div className="flex">
          <GoWorkflow className="h-4 w-4" />
        </div>
        <CardTitle>Transformer Mapping</CardTitle>
      </div>
      <CardDescription className="pt-2">
        Map Transformers to every column below.
      </CardDescription>
      <div className="z-50 pt-4">
        <SchemaTableToolbar<TData>
          table={table}
          displayApplyDefaultTransformersButton={
            displayApplyDefaultTransformersButton
          }
          isApplyDefaultButtonDisabled={isApplyDefaultTransformerButtonDisabled}
          onApplyDefaultClick={onApplyDefaultClick}
          onExportMappingsClick={(shouldFormat) =>
            onExportMappingsClick(
              table.getSelectedRowModel().rows,
              shouldFormat
            )
          }
          onImportMappingsClick={onImportMappingsClick}
          hasMissingSourceColumnMappings={hasMissingSourceColumnMappings}
          onRemoveMissingSourceColumnMappings={
            onRemoveMissingSourceColumnMappings
          }
          showPiiScan={showPiiScan}
          onScanContent={onScanContent}
          isScanningPii={isScanningPii}
        />
      </div>

      {/* useColumnSizes : par défaut FastTable impose 187px à chaque colonne, ce
          qui dépasse la largeur de l'écran. L'option applique la `size` déclarée
          par chaque colonne (cf. Columns.tsx), dimensionnées pour tenir sans
          défilement horizontal, et aligne l'en-tête sur les valeurs. */}
      <FastTable
        table={table}
        estimateRowSize={() => 53}
        rowOverscan={50}
        useColumnSizes
        // Les colonnes se partagent toute la largeur proportionnellement à leur
        // `size`. Seules la case à cocher et le bouton d'aperçu gardent une
        // largeur fixe : ce sont des icônes, les étirer ne servirait à rien.
        noGrowColumnIds={NO_GROW_COLUMNS}
        onRowClick={getColumnDecision ? onOpenDecision : undefined}
      />

      <SelectionBar
        count={selectedRows.length}
        getAllowedTransformers={() =>
          getAvalableTransformersForBulk(selectedRows)
        }
        getTransformerFromFieldValue={getTransformerFromFieldValue}
        onApply={(value) => {
          onTransformerBulkUpdate(
            selectedRows.map((r) => r.index),
            value
          );
          table.resetRowSelection(true);
        }}
        onClear={() => table.resetRowSelection(true)}
      />

      {target && opened !== null && (
        <MappingDecisionPanel
          key={opened}
          target={target}
          getTransformers={() => getAvailableTransformers(opened)}
          getTransformerFromFieldValue={getTransformerFromFieldValue}
          sourceConnectionId={sourceConnectionId}
          navigation={{
            position: position + 1,
            total: visible.length,
            onPrevious:
              position > 0
                ? () => setOpened(visible[position - 1].index)
                : undefined,
            onNext:
              position >= 0 && position < visible.length - 1
                ? () => setOpened(visible[position + 1].index)
                : undefined,
          }}
          onClose={closeDecision}
          onApply={(transformer) => onTransformerUpdate(opened, transformer)}
        />
      )}

      <div className="text-xs text-gray-600 dark:text-gray-400 pt-4">
        Total rows: ({getFormattedCount(data.length)}) Rows visible: (
        {getFormattedCount(table.getRowModel().rows.length)})
      </div>
    </div>
  );
}

// Défini hors du composant : une nouvelle référence à chaque rendu invaliderait
// la mémoïsation des lignes (cf. shouldReRender dans MemoizedRow).
const NO_GROW_COLUMNS = ['isSelected', 'openDecision'];

const US_NUMBER_FORMAT = new Intl.NumberFormat('en-US');
function getFormattedCount(count: number): string {
  return US_NUMBER_FORMAT.format(count);
}
