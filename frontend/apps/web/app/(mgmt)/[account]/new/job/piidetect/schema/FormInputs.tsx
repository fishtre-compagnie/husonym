import DualListBox, {
  EmptyStateMessage,
  Option,
} from '@/components/DualListBox/DualListBox';
import FormErrorMessage from '@/components/FormErrorMessage';
import FormHeader from '@/components/forms/FormHeader';
import { useAccount } from '@/components/providers/account-provider';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Label } from '@/components/ui/label';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import {
  Select,
  SelectContent,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { cn, splitConnections } from '@/libs/utils';
import { useQuery } from '@connectrpc/connect-query';
import { ConnectionDataService, ConnectionService } from '@husonym/sdk';
import { ReloadIcon, TableIcon } from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import { ReactElement, useCallback, useMemo } from 'react';
import ConnectionSelectContent from '../../connect/ConnectionSelectContent';
import {
  DataSamplingFormValue,
  FilterPatternTableIdentifier,
  IncrementalFormValue,
  TableScanFilterModeFormValue,
  TableScanFilterPatternsFormValue,
} from '../../job-form-validations';

interface SourceConnectionIdProps {
  error?: string;
  value: string;
  onChange(value: string): void;
}

export function SourceConnectionId(
  props: SourceConnectionIdProps
): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('newJob.piiDetect.form');

  const { account } = useAccount();
  const {
    data: connectionsResp,
    isLoading,
    isPending,
  } = useQuery(
    ConnectionService.method.getConnections,
    {
      accountId: account?.id,
    },
    { enabled: !!account?.id }
  );

  const connections = useMemo(() => {
    if (isLoading || isPending || !connectionsResp) {
      return { postgres: [], mysql: [], mssql: [] };
    }
    return splitConnections(connectionsResp.connections);
  }, [connectionsResp, isLoading, isPending]);

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('connection.title')}
        description={t('connection.description')}
        isErrored={!!error}
        labelClassName="text-lg"
      />
      <Select
        value={value}
        onValueChange={(value) => {
          if (!value) {
            return;
          }
          onChange(value);
        }}
      >
        <SelectTrigger>
          <SelectValue placeholder={t('connection.placeholder')} />
        </SelectTrigger>
        <SelectContent>
          <ConnectionSelectContent
            postgres={connections.postgres}
            mysql={connections.mysql}
            mssql={connections.mssql}
          />
        </SelectContent>
      </Select>
      <FormErrorMessage message={error} />
    </div>
  );
}

interface UserPromptProps {
  error?: string;
  value: string;
  onChange(value: string): void;
}

export function UserPrompt(props: UserPromptProps): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('newJob.piiDetect.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('userPrompt.title')}
        description={t('userPrompt.description')}
        isErrored={!!error}
        labelClassName="text-lg"
      />
      <Textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={t('userPrompt.placeholder')}
      />
      <FormErrorMessage message={error} />
    </div>
  );
}

interface DataSamplingProps {
  errors?: Record<string, string>;
  value: DataSamplingFormValue;
  onChange(value: DataSamplingFormValue): void;
}

export function DataSampling(props: DataSamplingProps): ReactElement {
  const { errors, value, onChange } = props;
  const t = useTranslations('newJob.piiDetect.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('dataSampling.title')}
        description={t('dataSampling.description')}
        isErrored={!!errors?.['isEnabled']}
        labelClassName="text-lg"
      />
      <ToggleGroup
        className="flex justify-start"
        type="single"
        onValueChange={(newValue) => {
          onChange({ ...value, isEnabled: newValue === 'enabled' });
        }}
        value={value.isEnabled ? 'enabled' : 'disabled'}
      >
        <ToggleGroupItem value="enabled">{t('enabled')}</ToggleGroupItem>
        <ToggleGroupItem value="disabled">{t('disabled')}</ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={errors?.['isEnabled']} />
      {value.isEnabled && (
        <div className="flex flex-col gap-4">
          <FormHeader
            title={t('dataSampling.modelInput.title')}
            description={t('dataSampling.modelInput.description')}
            isErrored={!!errors?.['modelInput']}
            labelClassName="text-base"
          />
          <RadioGroup
            onValueChange={(newValue) => {
              onChange({
                ...value,
                modelInput: newValue === 'values' ? 'values' : 'profiles',
              });
            }}
            value={value.modelInput === 'values' ? 'values' : 'profiles'}
          >
            <ModelInputRadioItem
              value="profiles"
              label={t('dataSampling.modelInput.profiles')}
              description={t('dataSampling.modelInput.profilesDescription')}
            />
            <ModelInputRadioItem
              value="values"
              label={t('dataSampling.modelInput.values')}
              description={t('dataSampling.modelInput.valuesDescription')}
            />
          </RadioGroup>
          <FormErrorMessage message={errors?.['modelInput']} />
          <p className="text-[0.8rem] text-muted-foreground">
            {t('dataSampling.noModel')}
          </p>
        </div>
      )}
    </div>
  );
}

interface ModelInputRadioItemProps {
  value: DataSamplingFormValue['modelInput'];
  label: string;
  description: string;
}

function ModelInputRadioItem(props: ModelInputRadioItemProps): ReactElement {
  const { value, label, description } = props;
  const id = `data-sampling-model-input-${value}`;
  return (
    <div className="flex items-start gap-2">
      <RadioGroupItem value={value} id={id} className="mt-1 cursor-pointer" />
      <div className="flex flex-col gap-1">
        <Label htmlFor={id} className="cursor-pointer">
          {label}
        </Label>
        <p className="text-[0.8rem] text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}

interface IncrementalProps {
  errors?: Record<string, string>;
  value: IncrementalFormValue;
  onChange(value: IncrementalFormValue): void;
}

export function Incremental(props: IncrementalProps): ReactElement {
  const { errors, value, onChange } = props;
  const t = useTranslations('newJob.piiDetect.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('incremental.title')}
        description={t('incremental.description')}
        isErrored={!!errors?.['isEnabled']}
        labelClassName="text-lg"
      />
      <ToggleGroup
        className="flex justify-start"
        type="single"
        onValueChange={(value) => {
          onChange({ isEnabled: value === 'enabled' });
        }}
        value={value.isEnabled ? 'enabled' : 'disabled'}
      >
        <ToggleGroupItem value="enabled">{t('enabled')}</ToggleGroupItem>
        <ToggleGroupItem value="disabled">{t('disabled')}</ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={errors?.['isEnabled']} />
    </div>
  );
}

interface TableScanFilterModeProps {
  error?: string;
  value: TableScanFilterModeFormValue;
  onChange(value: TableScanFilterModeFormValue): void;
}

export function TableScanFilterMode(
  props: TableScanFilterModeProps
): ReactElement {
  const { error, value, onChange } = props;
  const t = useTranslations('newJob.piiDetect.form');

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('scanMode.title')}
        description={t('scanMode.description')}
        isErrored={!!error}
        labelClassName="text-lg"
      />
      <ToggleGroup
        className="flex justify-start"
        type="single"
        onValueChange={(value) => {
          if (value) {
            onChange(value as TableScanFilterModeFormValue);
          }
        }}
        value={value}
      >
        <ToggleGroupItem value="include_all">
          {t('scanMode.include_all')}
        </ToggleGroupItem>
        <ToggleGroupItem value="include">
          {t('scanMode.include')}
        </ToggleGroupItem>
        <ToggleGroupItem value="exclude">
          {t('scanMode.exclude')}
        </ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={error} />
    </div>
  );
}

interface TableScanFilterPatternsProps {
  errors?: Record<string, string>;
  value: TableScanFilterPatternsFormValue;
  onChange(value: TableScanFilterPatternsFormValue): void;
  mode: TableScanFilterModeFormValue;
  connectionId: string;
}

export function TableScanFilterPatterns(
  props: TableScanFilterPatternsProps
): ReactElement | null {
  const { errors, value, onChange, connectionId, mode } = props;
  const t = useTranslations('newJob.piiDetect.form');
  const schemasAndTablesResp = useGetAvailableSchemasAndTables(
    connectionId,
    mode !== 'include_all'
  );

  if (mode === 'include_all') {
    return null;
  }

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title={t('patterns.title')}
        description={t('patterns.description')}
        isErrored={
          !!errors?.['patterns.schemas'] || !!errors?.['patterns.tables']
        }
        labelClassName="text-lg"
      />
      <TableScanFilterPatternSchemas
        error={errors?.['patterns.schemas']}
        value={value.schemas}
        onChange={(newSchemas) => {
          onChange({ ...value, schemas: newSchemas });
        }}
        availableSchemas={schemasAndTablesResp.schemas}
        isFetchingAvailableSchemas={schemasAndTablesResp.isFetching}
        onRefreshSchemasClicked={schemasAndTablesResp.refresh}
        mode={mode}
      />
      <TableScanFilterPatternTables
        error={errors?.['patterns.tables']}
        value={value.tables}
        onChange={(newTables) => {
          onChange({ ...value, tables: newTables });
        }}
        availableTableIdentifiers={schemasAndTablesResp.tables}
        isFetchingAvailableTableIdentifiers={schemasAndTablesResp.isFetching}
        onRefreshIdentifiersClicked={schemasAndTablesResp.refresh}
        mode={mode}
      />
    </div>
  );
}

interface TableScanFilterPatternSchemasProps {
  error?: string;
  value: string[];
  onChange(value: string[]): void;
  availableSchemas: string[];
  isFetchingAvailableSchemas: boolean;
  onRefreshSchemasClicked(): void;
  mode: TableScanFilterModeFormValue;
}

function TableScanFilterPatternSchemas(
  props: TableScanFilterPatternSchemasProps
): ReactElement {
  const {
    error,
    value,
    onChange,
    availableSchemas,
    isFetchingAvailableSchemas,
    onRefreshSchemasClicked,
    mode,
  } = props;

  const dualListBoxOpts = useMemo((): Option[] => {
    return availableSchemas.map((schema) => ({
      value: schema,
    }));
  }, [availableSchemas]);

  const selectedSchemas = useMemo((): Set<string> => {
    return new Set(value);
  }, [value]);

  const onSelectedChange = useCallback(
    (value: Set<string>) => {
      onChange(Array.from(value));
    },
    [onChange]
  );

  function onRefreshClick(): void {
    if (!isFetchingAvailableSchemas) {
      onRefreshSchemasClicked();
    }
  }

  const t = useTranslations('newJob.piiDetect.form.schemas');
  const leftEmptyStates = useEmptyStates('schemas', 'left', mode);
  const rightEmptyStates = useEmptyStates('schemas', 'right', mode);
  const cardDescription = t(`description.${mode}`);

  return (
    <div className="flex flex-col md:flex-row gap-3">
      <Card className="w-full">
        <CardHeader className="flex flex-col gap-2">
          <div className="flex flex-row items-center gap-2">
            <div className="flex">
              <TableIcon className="h-4 w-4" />
            </div>
            <CardTitle>{t('title')}</CardTitle>
            <RefreshButton
              isFetching={isFetchingAvailableSchemas}
              onClick={() => onRefreshClick()}
            />
          </div>
          <CardDescription>{cardDescription}</CardDescription>
        </CardHeader>
        <CardContent>
          <DualListBox
            options={dualListBoxOpts}
            selected={selectedSchemas}
            onChange={onSelectedChange}
            mode={'many'}
            leftEmptyState={leftEmptyStates}
            rightEmptyState={rightEmptyStates}
          />
        </CardContent>
      </Card>
      <FormErrorMessage message={error} />
    </div>
  );
}

interface TableScanFilterPatternTablesProps {
  error?: string;
  value: FilterPatternTableIdentifier[];
  onChange(value: FilterPatternTableIdentifier[]): void;
  availableTableIdentifiers: FilterPatternTableIdentifier[];
  isFetchingAvailableTableIdentifiers: boolean;
  onRefreshIdentifiersClicked(): void;
  mode: TableScanFilterModeFormValue;
}

function TableScanFilterPatternTables(
  props: TableScanFilterPatternTablesProps
): ReactElement {
  const {
    error,
    value,
    onChange,
    availableTableIdentifiers,
    isFetchingAvailableTableIdentifiers,
    onRefreshIdentifiersClicked,
    mode,
  } = props;
  const dualListBoxOpts = useMemo((): Option[] => {
    return availableTableIdentifiers.map((tableIdentifier) => ({
      value: `${tableIdentifier.schema}.${tableIdentifier.table}`,
    }));
  }, [availableTableIdentifiers]);

  const selectedSchemas = useMemo((): Set<string> => {
    return new Set(
      value.map(
        (tableIdentifier) =>
          `${tableIdentifier.schema}.${tableIdentifier.table}`
      )
    );
  }, [value]);

  const onSelectedChange = useCallback(
    (value: Set<string>) => {
      onChange(
        Array.from(value).map((tableIdentifier) => {
          const [schema, table] = tableIdentifier.split('.');
          return { schema, table };
        })
      );
    },
    [onChange]
  );

  function onRefreshClick(): void {
    if (!isFetchingAvailableTableIdentifiers) {
      onRefreshIdentifiersClicked();
    }
  }

  const t = useTranslations('newJob.piiDetect.form.tables');
  const leftEmptyStates = useEmptyStates('tables', 'left', mode);
  const rightEmptyStates = useEmptyStates('tables', 'right', mode);
  const cardDescription = t(`description.${mode}`);
  return (
    <div className="flex flex-col md:flex-row gap-3">
      <Card className="w-full">
        <CardHeader className="flex flex-col gap-2">
          <div className="flex flex-row items-center gap-2">
            <div className="flex">
              <TableIcon className="h-4 w-4" />
            </div>
            <CardTitle>{t('title')}</CardTitle>
            <RefreshButton
              isFetching={isFetchingAvailableTableIdentifiers}
              onClick={() => onRefreshClick()}
            />
          </div>
          <CardDescription>{cardDescription}</CardDescription>
        </CardHeader>
        <CardContent>
          <DualListBox
            options={dualListBoxOpts}
            selected={selectedSchemas}
            onChange={onSelectedChange}
            mode={'many'}
            leftEmptyState={leftEmptyStates}
            rightEmptyState={rightEmptyStates}
          />
        </CardContent>
      </Card>
      <FormErrorMessage message={error} />
    </div>
  );
}

/** What the two lists of a schema or table picker say when they are empty. */
function useEmptyStates(
  kind: 'schemas' | 'tables',
  side: 'left' | 'right',
  mode: TableScanFilterModeFormValue
): EmptyStateMessage {
  const t = useTranslations('newJob.piiDetect.form');
  return {
    noOptions: t(`${kind}.noOptions`),
    noSelected: t(`${kind}.${side}.${mode}`),
  };
}

interface UseGetAvailableSchemasAndTablesResponse {
  schemas: string[];
  tables: FilterPatternTableIdentifier[];

  isFetching: boolean;
  refresh(): Promise<void>;
}

function useGetAvailableSchemasAndTables(
  connectionId: string,
  isValidMode: boolean
): UseGetAvailableSchemasAndTablesResponse {
  const {
    data: availableSchemasAndTables,
    isLoading,
    isPending,
    isFetching,
    refetch,
  } = useQuery(
    ConnectionDataService.method.getAllSchemasAndTables,
    {
      connectionId,
    },
    { enabled: !!connectionId && isValidMode }
  );

  return useMemo(() => {
    if (isLoading || isPending || !availableSchemasAndTables) {
      return {
        schemas: [],
        tables: [],
        isFetching,
        refresh: async () => {
          await refetch();
        },
      };
    }
    return {
      schemas: availableSchemasAndTables.schemas.map((schema) => schema.name),
      tables: availableSchemasAndTables.tables.map((table) => ({
        schema: table.schemaName,
        table: table.tableName,
      })),
      isFetching,
      refresh: async () => {
        await refetch();
      },
    };
  }, [availableSchemasAndTables, isLoading, isPending, isFetching, refetch]);
}

interface RefreshButtonProps {
  isFetching: boolean;
  onClick(): void;
}

function RefreshButton(props: RefreshButtonProps): ReactElement {
  const { isFetching, onClick } = props;

  return (
    <Button variant="ghost" size="icon" onClick={onClick} type="button">
      <ReloadIcon className={cn('h-4 w-4', isFetching && 'animate-spin')} />
    </Button>
  );
}
