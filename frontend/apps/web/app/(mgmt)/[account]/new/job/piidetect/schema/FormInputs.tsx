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
        title="Connection"
        description="The connection to use for the PII detection job."
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
          <SelectValue placeholder="Select a source connection" />
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

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title="User Prompt"
        description="Optionally input a prompt to guide the LLM part of the PII detection job."
        isErrored={!!error}
        labelClassName="text-lg"
      />
      <Textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Do NOT classify the following columns as PII or personally identifiable information: id, created_at, updated_at"
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

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title="Data Sampling"
        description="Let the job read rows of each table. Up to 200 rows per table are read by the worker, inside your deployment, to recognise formats such as email addresses, IBANs or card numbers. When disabled, no row is read: only table names, column names and column types are used."
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
        <ToggleGroupItem value="enabled">Enabled</ToggleGroupItem>
        <ToggleGroupItem value="disabled">Disabled</ToggleGroupItem>
      </ToggleGroup>
      <FormErrorMessage message={errors?.['isEnabled']} />
      {value.isEnabled && (
        <div className="flex flex-col gap-4">
          <FormHeader
            title="What the model receives"
            description="Choose what the language model is given about the sampled rows."
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
            value={value.modelInput}
          >
            <ModelInputRadioItem
              value="profiles"
              label="Statistics only"
              description="The language model receives the table name, the column names and types, and statistics computed from the sampled rows: counts, lengths, kinds of characters, formats. No value of your data is sent to the model."
            />
            <ModelInputRadioItem
              value="values"
              label="Statistics and sample values"
              description="In addition, the language model receives up to 5 distinct values of each column, each cut to 64 characters. These values leave the worker for the model endpoint configured on this deployment. Choose this only if that endpoint may receive this data, for example a model hosted inside your network."
            />
          </RadioGroup>
          <FormErrorMessage message={errors?.['modelInput']} />
          <p className="text-[0.8rem] text-muted-foreground">
            With no model configured, nothing is sent anywhere: detection uses
            the rules only.
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

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title="Incremental Scans"
        description="Allow the job to incrementally detect PII for new or changed tables. This will only scan tables that have been added or updated since the last successful run. New table detection is dependent on the scan mode and patterns selected below."
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
        <ToggleGroupItem value="enabled">Enabled</ToggleGroupItem>
        <ToggleGroupItem value="disabled">Disabled</ToggleGroupItem>
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

  return (
    <div className="flex flex-col gap-4">
      <FormHeader
        title="Table Scan Mode"
        description="The mode to use for the table scan filter. This will determine what schemas and tables will be scanned for PII."
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
        <ToggleGroupItem value="include_all">Include All</ToggleGroupItem>
        <ToggleGroupItem value="include">Include</ToggleGroupItem>
        <ToggleGroupItem value="exclude">Exclude</ToggleGroupItem>
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
        title="Patterns"
        description="The patterns to use for the table scan filter based on the mode selected. A combination of schemas and tables will be scanned based on the mode and selection."
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

  const leftEmptyStates = useGetSchemaLeftEmptyStates(mode);
  const rightEmptyStates = useGetSchemaRightEmptyStates(mode);
  const cardDescription = useSchemaCardDescription(mode);

  return (
    <div className="flex flex-col md:flex-row gap-3">
      <Card className="w-full">
        <CardHeader className="flex flex-col gap-2">
          <div className="flex flex-row items-center gap-2">
            <div className="flex">
              <TableIcon className="h-4 w-4" />
            </div>
            <CardTitle>Schema Selection</CardTitle>
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

function useGetSchemaLeftEmptyStates(
  mode: TableScanFilterModeFormValue
): EmptyStateMessage {
  return useMemo(() => {
    switch (mode) {
      case 'include_all':
        return {
          noOptions: 'Unable to load schemas or found none',
          noSelected: 'All schemas have been added!',
        };
      case 'include':
        return {
          noOptions: 'Unable to load schemas or found none',
          noSelected: 'All schemas available have been included!',
        };
      case 'exclude':
        return {
          noOptions: 'Unable to load schemas or found none',
          noSelected: 'All schemas available have been excluded!',
        };
    }
  }, [mode]);
}

function useGetSchemaRightEmptyStates(
  mode: TableScanFilterModeFormValue
): EmptyStateMessage {
  return useMemo(() => {
    switch (mode) {
      case 'include_all':
        return {
          noOptions: 'Unable to load schemas or found none',
          noSelected: 'All schemas have been added!',
        };
      case 'include':
        return {
          noOptions: 'Unable to load schemas or found none',
          noSelected: 'Add schemas to scan for PII!',
        };
      case 'exclude':
        return {
          noOptions: 'Unable to load schemas or found none',
          noSelected: 'Add schemas to exclude from PII scanning!',
        };
    }
  }, [mode]);
}
function useSchemaCardDescription(mode: TableScanFilterModeFormValue): string {
  return useMemo(() => {
    switch (mode) {
      case 'include_all':
        return 'Select all schemas to scan for PII.';
      case 'include':
        return 'Select schemas to scan for PII.';
      case 'exclude':
        return 'Select schemas to exclude from PII scanning.';
    }
  }, [mode]);
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

  const leftEmptyStates = useGetTableLeftEmptyStates(mode);
  const rightEmptyStates = useGetTableRightEmptyStates(mode);
  const cardDescription = useTableCardDescription(mode);
  return (
    <div className="flex flex-col md:flex-row gap-3">
      <Card className="w-full">
        <CardHeader className="flex flex-col gap-2">
          <div className="flex flex-row items-center gap-2">
            <div className="flex">
              <TableIcon className="h-4 w-4" />
            </div>
            <CardTitle>Table Selection</CardTitle>
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

function useTableCardDescription(mode: TableScanFilterModeFormValue): string {
  return useMemo(() => {
    switch (mode) {
      case 'include_all':
        return 'Select all tables to scan for PII.';
      case 'include':
        return 'Select tables to scan for PII.';
      case 'exclude':
        return 'Select tables to exclude from PII scanning.';
    }
  }, [mode]);
}

function useGetTableLeftEmptyStates(
  mode: TableScanFilterModeFormValue
): EmptyStateMessage {
  return useMemo(() => {
    switch (mode) {
      case 'include_all':
        return {
          noOptions: 'Unable to load tables or found none',
          noSelected: 'All tables have been added!',
        };
      case 'include':
        return {
          noOptions: 'Unable to load tables or found none',
          noSelected: 'All tables available have been included!',
        };
      case 'exclude':
        return {
          noOptions: 'Unable to load tables or found none',
          noSelected: 'All tables available have been excluded!',
        };
    }
  }, [mode]);
}

function useGetTableRightEmptyStates(
  mode: TableScanFilterModeFormValue
): EmptyStateMessage {
  return useMemo(() => {
    switch (mode) {
      case 'include_all':
        return {
          noOptions: 'Unable to load tables or found none',
          noSelected: 'All tables have been added!',
        };
      case 'include':
        return {
          noOptions: 'Unable to load tables or found none',
          noSelected: 'Add tables to scan for PII!',
        };
      case 'exclude':
        return {
          noOptions: 'Unable to load tables or found none',
          noSelected: 'Add tables to exclude from PII scanning!',
        };
    }
  }, [mode]);
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
