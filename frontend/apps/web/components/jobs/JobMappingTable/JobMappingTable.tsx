import FastTable from '@/components/FastTable/FastTable';
import { CardDescription, CardTitle } from '@/components/ui/card';
import { Transformer } from '@/shared/transformers';
import { JobMappingTransformerForm } from '@/yup-validations/jobs';
import { JobMapping } from '@husonym/sdk';
import {
  ColumnDef,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  Row,
  RowData,
  useReactTable,
} from '@tanstack/react-table';
import { ReactElement } from 'react';
import { GoWorkflow } from 'react-icons/go';
import { ImportMappingsConfig } from '../SchemaTable/ImportJobMappingsButton';
import { SchemaTableToolbar } from '../SchemaTable/SchemaTableToolBar';
import { TransformerResult } from '../SchemaTable/transformer-handler';

interface Props<TData, TValue> {
  data: TData[];
  columns: ColumnDef<TData, TValue>[];
  onTransformerUpdate(index: number, config: JobMappingTransformerForm): void;
  getAvailableTransformers(index: number): TransformerResult;
  getTransformerFromField(index: number): Transformer;

  onTransformerBulkUpdate(
    indices: number[],
    config: JobMappingTransformerForm
  ): void;
  getAvalableTransformersForBulk(rows: Row<TData>[]): TransformerResult;
  getTransformerFromFieldValue(value: JobMappingTransformerForm): Transformer;

  isApplyDefaultTransformerButtonDisabled: boolean;
  displayApplyDefaultTransformersButton: boolean;
  onApplyDefaultClick(override: boolean): void;

  onExportMappingsClick(selected: Row<TData>[], shouldFormat: boolean): void;
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

  // Id de la connexion source, propagé aux cellules via meta pour l'aperçu.
  sourceConnectionId?: string;

  // Scan de contenu PII (Presidio) — actif uniquement pour les jobs sync.
  showPiiScan?: boolean;
  onScanContent?(): void;
  isScanningPii?: boolean;
}

declare module '@tanstack/react-table' {
  interface TableMeta<TData extends RowData> {
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
      // alors aucune donnée à échantillonner, le bouton d'aperçu est masqué.
      sourceConnectionId?: string;
    };
  }
}

export default function JobMappingTable<TData, TValue>(
  props: Props<TData, TValue>
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
    showPiiScan,
    onScanContent,
    isScanningPii,
  } = props;

  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
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
      },
    },
  });

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
          getAllowedTransformers={getAvalableTransformersForBulk}
          getTransformerFromField={getTransformerFromFieldValue}
          onApplyDefaultClick={onApplyDefaultClick}
          onBulkUpdate={onTransformerBulkUpdate}
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
      />

      <div className="text-xs text-gray-600 dark:text-gray-400 pt-4">
        Total rows: ({getFormattedCount(data.length)}) Rows visible: (
        {getFormattedCount(table.getRowModel().rows.length)})
      </div>
    </div>
  );
}

// Défini hors du composant : une nouvelle référence à chaque rendu invaliderait
// la mémoïsation des lignes (cf. shouldReRender dans MemoizedRow).
const NO_GROW_COLUMNS = ['isSelected', 'preview'];

const US_NUMBER_FORMAT = new Intl.NumberFormat('en-US');
function getFormattedCount(count: number): string {
  return US_NUMBER_FORMAT.format(count);
}
