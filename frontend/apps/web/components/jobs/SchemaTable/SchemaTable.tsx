'use client';
import { SingleTableSchemaFormValues } from '@/app/(mgmt)/[account]/new/job/job-form-validations';
import DualListBox, {
  Action,
  Option,
} from '@/components/DualListBox/DualListBox';
import SkeletonTable from '@/components/skeleton/SkeletonTable';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Transformer } from '@/shared/transformers';
import {
  convertJobMappingTransformerToForm,
  JobMappingFormValues,
  JobMappingTransformerForm,
  SchemaFormValues,
  VirtualForeignConstraintFormValues,
} from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import {
  ConnectionDataService,
  GetConnectionSchemaResponse,
  JobMapping,
  JobMappingTransformerSchema,
  PiiConfidence,
  PiiDetectionMethod,
  TransformerSource,
  ValidateJobMappingsResponse,
} from '@husonym/sdk';
import { TableIcon } from '@radix-ui/react-icons';
import { Row } from '@tanstack/react-table';
import { ReactElement, useEffect, useMemo, useState } from 'react';
import { toast } from 'sonner';
import { FieldErrors } from 'react-hook-form';
import {
  getGeneratedStatement,
  getIdentityStatement,
} from '../JobMappingTable/AttributesCell';
import { JobMappingRow, SQL_COLUMNS } from '../JobMappingTable/Columns';
import JobMappingTable from '../JobMappingTable/JobMappingTable';
import FormErrorsCard, { ErrorLevel, FormError } from './FormErrorsCard';
import { ImportMappingsConfig } from './ImportJobMappingsButton';
import { getVirtualForeignKeysColumns } from './VirtualFkColumns';
import VirtualFkPageTable from './VirtualFkPageTable';
import { VirtualForeignKeyForm } from './VirtualForeignKeyForm';
import { JobType, SchemaConstraintHandler } from './schema-constraint-handler';
import { TransformerResult } from './transformer-handler';
import { useOnExportMappings } from './useOnExportMappings';
import { handleDataTypeBadge } from './util';

interface Props {
  data: JobMappingFormValues[];
  virtualForeignKeys?: VirtualForeignConstraintFormValues[];
  addVirtualForeignKey?: (vfk: VirtualForeignConstraintFormValues) => void;
  removeVirtualForeignKey?: (index: number) => void;
  jobType: JobType;
  schema: Record<string, GetConnectionSchemaResponse>;
  isSchemaDataReloading: boolean;
  constraintHandler: SchemaConstraintHandler;
  selectedTables: Set<string>;
  onSelectedTableToggle(items: Set<string>, action: Action): void;
  isJobMappingsValidating?: boolean;

  onValidate?(): void;

  formErrors: FormError[];
  onImportMappingsClick(
    jobmappings: JobMapping[],
    importConfig: ImportMappingsConfig
  ): void;
  onTransformerUpdate(index: number, config: JobMappingTransformerForm): void;
  getAvailableTransformers(index: number): TransformerResult;
  getTransformerFromField(index: number): Transformer;
  onTransformerBulkUpdate(
    indices: number[],
    config: JobMappingTransformerForm
  ): void;
  getAvailableTransformersForBulk(
    rows: Row<JobMappingRow>[]
  ): TransformerResult;
  getTransformerFromFieldValue(value: JobMappingTransformerForm): Transformer;
  onApplyDefaultClick(override: boolean): void;
  hasMissingSourceColumnMappings: boolean;
  onRemoveMissingSourceColumnMappings(): void;
  // Id de la connexion SOURCE. S'il est fourni (jobs sync), active le scan de
  // contenu PII (Presidio). Absent pour les jobs generate (rien à échantillonner).
  sourceConnectionId?: string;
}

// Détection remontée par le scan de contenu, telle que le backend l'a qualifiée.
interface ContentPii {
  source: TransformerSource;
  category?: string;
  isSensitive: boolean;
  confidence: PiiConfidence;
  method: PiiDetectionMethod;
  evidence: string;
}

interface ResolvedPii {
  isSensitive: boolean;
  suggestedTransformerSource: TransformerSource;
  dataCategory?: string;
  confidence: PiiConfidence;
  method: PiiDetectionMethod;
  evidence: string;
}

export function SchemaTable(props: Props): ReactElement {
  const {
    data,
    virtualForeignKeys,
    addVirtualForeignKey,
    removeVirtualForeignKey,
    constraintHandler,
    jobType,
    schema,
    selectedTables,
    onSelectedTableToggle,
    formErrors,
    isJobMappingsValidating,
    onValidate,
    onImportMappingsClick,
    onTransformerUpdate,
    getAvailableTransformers,
    getTransformerFromField,
    getAvailableTransformersForBulk,
    getTransformerFromFieldValue,
    onApplyDefaultClick,
    onTransformerBulkUpdate,
    hasMissingSourceColumnMappings,
    onRemoveMissingSourceColumnMappings,
    sourceConnectionId,
  } = props;

  // --- Scan de contenu PII (Presidio) ---------------------------------------
  const [contentPii, setContentPii] = useState<Record<string, ContentPii>>({});
  const [isScanningPii, setIsScanningPii] = useState(false);
  const { mutateAsync: detectPii } = useMutation(
    ConnectionDataService.method.detectPiiInConnectionData
  );

  // Fusionne détection par NOM (constraintHandler, déterministe) et détection de
  // CONTENU (scan, qualifiée par le backend). Le nom prime : c'est une preuve
  // reproductible qui ne dépend d'aucun modèle. Le contenu comble les colonnes
  // que le nom a manquées — typiquement celles nommées col_1, col_2...
  const resolvePiiWith = (
    content: Record<string, ContentPii>,
    colKey: { schema: string; table: string; column: string }
  ): ResolvedPii => {
    const nameSensitive = constraintHandler.getIsSensitive(colKey);
    const nameSource = constraintHandler.getSuggestedTransformerSource(colKey);
    const nameCategory = constraintHandler.getDataCategory(colKey);
    const c = content[`${colKey.schema}.${colKey.table}.${colKey.column}`];
    if (nameSensitive) {
      // Le nom a établi la NATURE de la donnée. Mais un doute sur le FORMAT est
      // une autre question : « c'est bien une date de naissance » n'implique pas
      // « on sait dans quel format la réécrire ». Une date jj/mm indistinguable
      // de mm/jj doit être signalée même si la colonne s'appelle
      // date_naissance, sinon on écrirait la base cible dans le mauvais format.
      const formatDoubt =
        c?.confidence === PiiConfidence.NEEDS_REVIEW &&
        c.method === PiiDetectionMethod.FORMAT;
      return {
        isSensitive: true,
        suggestedTransformerSource: nameSource,
        dataCategory: nameCategory,
        confidence: formatDoubt
          ? PiiConfidence.NEEDS_REVIEW
          : PiiConfidence.CONFIRMED,
        method: formatDoubt
          ? PiiDetectionMethod.FORMAT
          : PiiDetectionMethod.COLUMN_NAME,
        evidence: formatDoubt
          ? (c?.evidence ?? '')
          : `reconnu par le nom de colonne « ${colKey.column} »`,
      };
    }
    if (c) {
      return {
        isSensitive: c.isSensitive,
        suggestedTransformerSource: c.source,
        dataCategory: c.category,
        confidence: c.confidence,
        method: c.method,
        evidence: c.evidence,
      };
    }
    return {
      isSensitive: false,
      suggestedTransformerSource: nameSource,
      dataCategory: nameCategory,
      confidence: PiiConfidence.UNSPECIFIED,
      method: PiiDetectionMethod.UNSPECIFIED,
      evidence: '',
    };
  };

  const resolvePii = (colKey: {
    schema: string;
    table: string;
    column: string;
  }): ResolvedPii => resolvePiiWith(contentPii, colKey);

  // Applique automatiquement le transformer suggéré par la détection DÉTERMINISTE
  // (par NOM) aux colonnes encore en passthrough (jamais d'écrasement d'un choix
  // explicite). La détection par CONTENU (Presidio) est faillible : elle ne fait
  // qu'ALERTER (badge), sans jamais modifier le transformer.
  const applyNamePiiSuggestions = (): void => {
    data.forEach((d, idx) => {
      const colKey = { schema: d.schema, table: d.table, column: d.column };
      if (!constraintHandler.getIsSensitive(colKey)) {
        return;
      }
      const source = constraintHandler.getSuggestedTransformerSource(colKey);
      if (source === TransformerSource.UNSPECIFIED) {
        return;
      }
      const currentCase = d.transformer?.config?.case;
      if (currentCase && currentCase !== 'passthroughConfig') {
        return; // choix explicite : on ne touche pas
      }
      const sys = getAvailableTransformers(idx).system.find(
        (t) => t.source === source
      );
      if (!sys) {
        return;
      }
      onTransformerUpdate(
        idx,
        convertJobMappingTransformerToForm(
          create(JobMappingTransformerSchema, { config: sys.config })
        )
      );
    });
  };

  // Applique le transformer suggéré par une détection de contenu CONFIRMÉE
  // (clé de contrôle vérifiée). Ne touche jamais une colonne dont le transformer
  // a déjà été choisi explicitement. Retourne le nombre de colonnes modifiées.
  const applyContentPiiSuggestions = (
    content: Record<string, ContentPii>
  ): number => {
    let applied = 0;
    data.forEach((d, idx) => {
      const c = content[`${d.schema}.${d.table}.${d.column}`];
      if (!c || c.confidence !== PiiConfidence.CONFIRMED) {
        return;
      }
      if (c.source === TransformerSource.UNSPECIFIED) {
        return; // pas de générateur adapté (IBAN, SIRET, date...)
      }
      const currentCase = d.transformer?.config?.case;
      if (currentCase && currentCase !== 'passthroughConfig') {
        return;
      }
      const sys = getAvailableTransformers(idx).system.find(
        (t) => t.source === c.source
      );
      if (!sys) {
        return;
      }
      onTransformerUpdate(
        idx,
        convertJobMappingTransformerToForm(
          create(JobMappingTransformerSchema, { config: sys.config })
        )
      );
      applied++;
    });
    return applied;
  };

  const onScanContent = async (): Promise<void> => {
    if (!sourceConnectionId) {
      return;
    }
    setIsScanningPii(true);
    try {
      const tables = new Map<string, { schema: string; table: string }>();
      data.forEach((d) =>
        tables.set(`${d.schema}.${d.table}`, {
          schema: d.schema,
          table: d.table,
        })
      );
      const next: Record<string, ContentPii> = {};
      for (const { schema: sch, table: tbl } of tables.values()) {
        const resp = await detectPii({
          connectionId: sourceConnectionId,
          schema: sch,
          table: tbl,
          sampleSize: 20,
        });
        resp.detections.forEach((det) => {
          next[`${det.schema}.${det.table}.${det.column}`] = {
            source: det.suggestedTransformerSource,
            category: det.dataCategory,
            isSensitive: det.isSensitive,
            confidence: det.piiConfidence,
            method: det.piiDetectionMethod,
            evidence: det.piiEvidence,
          };
        });
      }
      setContentPii(next);

      // Les détections prouvées par une clé de contrôle (NIR mod 97, IBAN,
      // Luhn...) sont appliquées comme celles issues du nom. Celles qui reposent
      // sur un modèle statistique ou un format ambigu ne le sont jamais : elles
      // s'affichent en badge orange pour que l'utilisateur lève le doute.
      const applied = applyContentPiiSuggestions(next);

      const confirmed = Object.values(next).filter(
        (c) => c.confidence === PiiConfidence.CONFIRMED
      ).length;
      const toReview = Object.values(next).filter(
        (c) => c.confidence === PiiConfidence.NEEDS_REVIEW
      ).length;

      if (confirmed === 0 && toReview === 0) {
        toast.success(
          'Aucune donnée personnelle détectée dans le contenu échantillonné.'
        );
      } else {
        const parts: string[] = [];
        if (confirmed > 0) {
          parts.push(
            `${confirmed} colonne(s) confirmée(s) par clé de contrôle` +
              (applied > 0 ? ` (${applied} transformer(s) appliqué(s))` : '')
          );
        }
        if (toReview > 0) {
          parts.push(`${toReview} à vérifier`);
        }
        toast.success(parts.join(' · '));
      }
    } catch (e) {
      toast.error(
        `Échec du scan de contenu : ${e instanceof Error ? e.message : 'erreur inconnue'}`
      );
    } finally {
      setIsScanningPii(false);
    }
  };

  // Auto-application des suggestions par NOM au chargement (jobs sync). Le garde
  // « passthrough uniquement » rend l'opération idempotente (pas de boucle).
  useEffect(() => {
    if (!sourceConnectionId) {
      return;
    }
    applyNamePiiSuggestions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [constraintHandler, sourceConnectionId]);

  const piiScanProps = {
    showPiiScan: !!sourceConnectionId,
    onScanContent,
    isScanningPii,
    // Propagé jusqu'aux cellules (via meta) pour l'aperçu des valeurs.
    sourceConnectionId,
  };
  // --------------------------------------------------------------------------

  const tableData = useMemo((): JobMappingRow[] => {
    return data.map((d): JobMappingRow => {
      const colKey = {
        schema: d.schema,
        table: d.table,
        column: d.column,
      };
      const isPrimaryKey = constraintHandler.getIsPrimaryKey(colKey);
      const [isForeignKey, fkCols] = constraintHandler.getIsForeignKey(colKey);
      const [isVirtualForeignKey, vfkCols] =
        constraintHandler.getIsVirtualForeignKey(colKey);
      const isUnique = constraintHandler.getIsUniqueConstraint(colKey);

      const constraintPieces: string[] = [];
      if (isPrimaryKey) {
        constraintPieces.push('Primary Key');
      }
      if (isForeignKey) {
        fkCols.forEach((col) => constraintPieces.push(`Foreign Key: ${col}`));
      }
      if (isVirtualForeignKey) {
        vfkCols.forEach((col) =>
          constraintPieces.push(`Virtual Foreign Key: ${col}`)
        );
      }
      if (isUnique) {
        constraintPieces.push('Unique');
      }
      const constraints = constraintPieces.join('\n');

      const generatedType = constraintHandler.getGeneratedType(colKey);
      const identityType = constraintHandler.getIdentityType(colKey);

      const attributePieces: string[] = [];
      if (generatedType) {
        attributePieces.push(getGeneratedStatement(generatedType));
      } else if (identityType) {
        attributePieces.push(getIdentityStatement(identityType));
      }
      attributePieces.push(
        constraintHandler.getIsNullable(colKey) ? 'Is Nullable' : 'Not Nullable'
      );
      const attributes = attributePieces.join('\n');

      return {
        schema: d.schema,
        table: d.table,
        column: d.column,
        dataType: handleDataTypeBadge(constraintHandler.getDataType(colKey)),
        attributes: {
          value: attributes,
          generatedType: generatedType,
          identityType: identityType,
        },
        constraints: {
          value: constraints,
          foreignKey: [isForeignKey, fkCols],
          virtualForeignKey: [isVirtualForeignKey, vfkCols],
          isPrimaryKey: isPrimaryKey,
          isUnique: isUnique,
        },
        isNullable: constraintHandler.getIsNullable(colKey),
        transformer: d.transformer,
        ...(() => {
          const pii = resolvePii(colKey);
          return {
            isSensitive: pii.isSensitive,
            dataCategory: pii.dataCategory,
            suggestedTransformerSource: pii.suggestedTransformerSource,
            piiConfidence: pii.confidence,
            piiDetectionMethod: pii.method,
            piiEvidence: pii.evidence,
          };
        })(),
      };
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data, constraintHandler, contentPii]);

  const virtualForeignKeyColumns = useMemo(() => {
    return getVirtualForeignKeysColumns({ removeVirtualForeignKey });
  }, [removeVirtualForeignKey]);

  // it is imperative that this is stable to not cause infinite re-renders of the listbox(s)
  const dualListBoxOpts = useMemo(
    () => getDualListBoxOptions(new Set(Object.keys(schema)), data),
    [schema, data]
  );

  const { onClick: onExportMappingsClick } = useOnExportMappings<JobMappingRow>(
    {
      jobMappings: data,
    }
  );

  if (!data) {
    return <SkeletonTable />;
  }

  return (
    <div className="flex flex-col gap-10">
      <div className="flex flex-col md:flex-row gap-3">
        <Card className="w-full">
          <CardHeader className="flex flex-col gap-2">
            <div className="flex flex-row items-center gap-2">
              <div className="flex">
                <TableIcon className="h-4 w-4" />
              </div>
              <CardTitle>Table Selection</CardTitle>
            </div>
            <CardDescription>
              Select the tables that you want to transform and move them from
              the source to destination table.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <DualListBox
              options={dualListBoxOpts}
              selected={selectedTables}
              onChange={onSelectedTableToggle}
              mode={jobType === 'generate' ? 'single' : 'many'}
            />
          </CardContent>
        </Card>
        <FormErrorsCard
          formErrors={formErrors}
          isValidating={isJobMappingsValidating}
          onValidate={onValidate}
        />
      </div>

      {virtualForeignKeys && addVirtualForeignKey ? (
        <Tabs defaultValue="mappings">
          <TabsList>
            <TabsTrigger value="mappings">Transformer Mappings</TabsTrigger>
            <TabsTrigger value="virtualforeignkeys">
              Virtual Foreign Keys
            </TabsTrigger>
          </TabsList>
          <TabsContent value="mappings">
            <JobMappingTable
              data={tableData}
              columns={SQL_COLUMNS}
              displayApplyDefaultTransformersButton={jobType === 'sync'}
              onTransformerUpdate={onTransformerUpdate}
              getAvailableTransformers={getAvailableTransformers}
              getTransformerFromField={getTransformerFromField}
              onExportMappingsClick={onExportMappingsClick}
              onImportMappingsClick={onImportMappingsClick}
              isApplyDefaultTransformerButtonDisabled={data.length === 0}
              getAvalableTransformersForBulk={getAvailableTransformersForBulk}
              getTransformerFromFieldValue={getTransformerFromFieldValue}
              onTransformerBulkUpdate={onTransformerBulkUpdate}
              onApplyDefaultClick={onApplyDefaultClick}
              onDeleteRow={() =>
                console.warn('on delete row is not implemented')
              }
              onDuplicateRow={() =>
                console.warn('on duplicate row is not implemented')
              }
              canRenameColumn={() => false}
              onRowUpdate={() => console.warn('onRowUpdate is not implemented')}
              getAvailableCollectionsByRow={() => {
                console.warn('getAvailableCollections is not implemented');
                return [];
              }}
              hasMissingSourceColumnMappings={hasMissingSourceColumnMappings}
              onRemoveMissingSourceColumnMappings={
                onRemoveMissingSourceColumnMappings
              }
              {...piiScanProps}
            />
          </TabsContent>
          <TabsContent value="virtualforeignkeys">
            <div className="flex flex-col gap-6 pt-4">
              <VirtualForeignKeyForm
                schema={schema}
                constraintHandler={constraintHandler}
                selectedTables={selectedTables}
                addVirtualForeignKey={addVirtualForeignKey}
              />
              <VirtualFkPageTable
                columns={virtualForeignKeyColumns}
                data={virtualForeignKeys}
              />
            </div>
          </TabsContent>
        </Tabs>
      ) : (
        <JobMappingTable
          data={tableData}
          columns={SQL_COLUMNS}
          displayApplyDefaultTransformersButton={jobType === 'sync'}
          onTransformerUpdate={onTransformerUpdate}
          getAvailableTransformers={getAvailableTransformers}
          getTransformerFromField={getTransformerFromField}
          onExportMappingsClick={onExportMappingsClick}
          onImportMappingsClick={onImportMappingsClick}
          isApplyDefaultTransformerButtonDisabled={data.length === 0}
          getAvalableTransformersForBulk={getAvailableTransformersForBulk}
          getTransformerFromFieldValue={getTransformerFromFieldValue}
          onTransformerBulkUpdate={onTransformerBulkUpdate}
          onApplyDefaultClick={onApplyDefaultClick}
          onDeleteRow={() => console.warn('on delete row is not implemented')}
          onDuplicateRow={() =>
            console.warn('on duplicate row is not implemented')
          }
          canRenameColumn={() => false}
          onRowUpdate={() => console.warn('onRowUpdate is not implemented')}
          getAvailableCollectionsByRow={() => {
            console.warn('getAvailableCollections is not implemented');
            return [];
          }}
          hasMissingSourceColumnMappings={hasMissingSourceColumnMappings}
          onRemoveMissingSourceColumnMappings={
            onRemoveMissingSourceColumnMappings
          }
          {...piiScanProps}
        />
      )}
    </div>
  );
}

function getDualListBoxOptions(
  tables: Set<string>,
  jobmappings: JobMappingFormValues[]
): Option[] {
  jobmappings.forEach((jm) => tables.add(`${jm.schema}.${jm.table}`));
  return Array.from(tables).map((table): Option => ({ value: table }));
}

function extractAllFormErrors(
  errors: FieldErrors<SchemaFormValues | SingleTableSchemaFormValues>,
  values: JobMappingFormValues[],
  path = ''
): FormError[] {
  let messages: FormError[] = [];

  for (const key in errors) {
    let newPath = path;

    if (!isNaN(Number(key))) {
      const index = Number(key);
      if (index < values.length) {
        const value = values[index];
        const column = `${value.schema}.${value.table}.${value.column}`;
        newPath = path ? `${path}.${column}` : column;
      }
    }
    const error = (errors as any)[key as unknown as any] as any; // eslint-disable-line @typescript-eslint/no-explicit-any

    if (!error) {
      continue;
    }
    if (error.message) {
      messages.push({
        path: newPath,
        message: error.message,
        type: error.type,
        level: 'error',
      });
    } else {
      messages = messages.concat(extractAllFormErrors(error, values, newPath));
    }
  }
  return messages;
}

export function getAllFormErrors(
  formErrors: FieldErrors<SchemaFormValues | SingleTableSchemaFormValues>,
  values: JobMappingFormValues[],
  validationErrors: ValidateJobMappingsResponse | undefined
): FormError[] {
  let messages: FormError[] = [];
  const formErr = extractAllFormErrors(formErrors, values);
  if (!validationErrors) {
    return formErr;
  }
  const colErr = validationErrors.columnErrors.map((e) => {
    return {
      path: `${e.schema}.${e.table}.${e.column}`,
      message: e.errors.join('. '),
      level: 'error' as ErrorLevel,
    };
  });
  const colWarnings = validationErrors.columnWarnings.map((e) => {
    return {
      path: `${e.schema}.${e.table}.${e.column}`,
      message: e.warningReports.map((w) => w.message).join('. '),
      level: 'warning' as ErrorLevel,
    };
  });
  const dbErr = validationErrors.databaseErrors?.errorReports.map((e) => {
    return {
      path: '',
      message: e.message,
      level: 'error' as ErrorLevel,
    };
  });
  messages = messages.concat(colErr, formErr, colWarnings);
  if (dbErr) {
    messages = messages.concat(dbErr);
  }

  return messages;
}
