'use client';

import { ReactTable, RowData } from '@tanstack/react-table';

import ButtonText from '@/components/ButtonText';
import { AppTableFeatures } from '@/components/table/features';
import { Button } from '@/components/ui/button';
import { JobMapping } from '@husonym/sdk';
import {
  Cross2Icon,
  MagnifyingGlassIcon,
  ReloadIcon,
} from '@radix-ui/react-icons';
import ApplyDefaultTransformersButton from './ApplyDefaultTransformersButton';
import ExportJobMappingsButton from './ExportJobMappingsButton';
import ImportJobMappingsButton, {
  ImportMappingsConfig,
} from './ImportJobMappingsButton';
import { SchemaTableViewOptions } from './SchemaTableViewOptions';

interface DataTableToolbarProps<TData extends RowData> {
  table: ReactTable<AppTableFeatures, TData>;
  onExportMappingsClick(shouldFormat: boolean): void;
  onImportMappingsClick(
    jobmappings: JobMapping[],
    config: ImportMappingsConfig
  ): void;
  displayApplyDefaultTransformersButton: boolean;
  isApplyDefaultButtonDisabled: boolean;
  onApplyDefaultClick(override: boolean): void;

  hasMissingSourceColumnMappings: boolean;
  onRemoveMissingSourceColumnMappings(): void;

  // Scan de contenu PII (Presidio) — présent seulement pour les jobs sync.
  showPiiScan?: boolean;
  onScanContent?(): void;
  isScanningPii?: boolean;
}

export function SchemaTableToolbar<TData extends RowData>({
  table,
  onExportMappingsClick,
  onImportMappingsClick,
  displayApplyDefaultTransformersButton,
  isApplyDefaultButtonDisabled,
  onApplyDefaultClick,
  hasMissingSourceColumnMappings,
  onRemoveMissingSourceColumnMappings,
  showPiiScan,
  onScanContent,
  isScanningPii,
}: DataTableToolbarProps<TData>) {
  const isFiltered = table.state.columnFilters.length > 0;

  return (
    <div className="flex flex-col items-start w-full gap-2">
      <div className="flex flex-row justify-end pb-2 items-center w-full gap-3">
        <div className="flex flex-col md:flex-row md:items-center gap-2">
          {isFiltered && (
            <Button
              variant="outline"
              type="button"
              onClick={() => {
                table.resetColumnFilters();
              }}
              className="h-8 px-2 lg:px-3"
            >
              <ButtonText
                leftIcon={<Cross2Icon className="h-3 w-3" />}
                text="Clear filters"
              />
            </Button>
          )}
          {hasMissingSourceColumnMappings && (
            <Button
              variant="outline"
              type="button"
              disabled={!hasMissingSourceColumnMappings}
              onClick={onRemoveMissingSourceColumnMappings}
            >
              <ButtonText text="Remove Missing Source Column Mappings" />
            </Button>
          )}
          {showPiiScan && (
            <Button
              variant="outline"
              type="button"
              disabled={isScanningPii}
              onClick={() => onScanContent?.()}
              title="Analyse le contenu échantillonné des colonnes (Presidio) pour détecter des données personnelles, y compris dans des colonnes mal nommées. Les transformers suggérés sont appliqués automatiquement."
            >
              <ButtonText
                leftIcon={
                  isScanningPii ? (
                    <ReloadIcon className="h-3 w-3 animate-spin" />
                  ) : (
                    <MagnifyingGlassIcon className="h-3 w-3" />
                  )
                }
                text={
                  isScanningPii ? 'Scan en cours…' : 'Scan de contenu (RGPD)'
                }
              />
            </Button>
          )}
          {displayApplyDefaultTransformersButton && (
            <ApplyDefaultTransformersButton
              isDisabled={isApplyDefaultButtonDisabled}
              onClick={onApplyDefaultClick}
            />
          )}
          <ImportJobMappingsButton onImport={onImportMappingsClick} />
          <ExportJobMappingsButton
            onClick={onExportMappingsClick}
            count={table.getSelectedRowModel().rows.length}
          />
          <SchemaTableViewOptions table={table} />
        </div>
      </div>
    </div>
  );
}
