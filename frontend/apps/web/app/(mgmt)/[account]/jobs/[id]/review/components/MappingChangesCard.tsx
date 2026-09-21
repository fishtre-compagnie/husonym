import { getConnectionIdFromSource } from '@/app/(mgmt)/[account]/jobs/[id]/source/components/util';
import EditTransformerOptions from '@/app/(mgmt)/[account]/transformers/EditTransformerOptions';
import ColumnPreviewDialog from '@/components/jobs/JobMappingTable/ColumnPreviewDialog';
import { dbDataTypeToTransformerDataType } from '@/components/jobs/SchemaTable/schema-constraint-handler';
import TransformerSelect from '@/components/jobs/SchemaTable/TransformerSelect';
import { useAccount } from '@/components/providers/account-provider';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { useGetTransformersHandler } from '@/libs/hooks/useGetTransformersHandler';
import {
  formatDateTime,
  getErrorMessage,
  getFilterdTransformersByType,
  getTransformerFromField,
  getTransformerSelectButtonText,
  isInvalidTransformer,
} from '@/util/util';
import {
  changeLabel,
  columnName,
  isPassthrough,
  urgency,
} from '@/util/mapping-changes';
import {
  convertJobMappingTransformerFormToJobMappingTransformer,
  convertJobMappingTransformerToForm,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import {
  createConnectQueryKey,
  useMutation,
  useQuery,
} from '@connectrpc/connect-query';
import {
  JobMappingChange,
  JobMappingChangeKind,
  JobMappingSchema,
  JobService,
} from '@husonym/sdk';
import { CheckCircledIcon, EyeOpenIcon } from '@radix-ui/react-icons';
import { useQueryClient } from '@tanstack/react-query';
import { ReactElement, useMemo, useState } from 'react';
import { toast } from 'sonner';
import ReviewChangesDialog, { ReviewAction } from './ReviewChangesDialog';

interface Props {
  jobId: string;
}

// A change whose column is still mapped: its transformer can be corrected from here.
function isCorrectable(c: JobMappingChange): boolean {
  return c.kind !== JobMappingChangeKind.REMOVED;
}

const NO_TRANSFORMER: JobMappingTransformerForm = {
  config: { case: '', value: {} },
};

// What the job's runs changed in its mappings and nobody has reviewed yet: the columns they
// mapped — with the transformer they chose, or in clear when none applied — the mappings they
// removed with their column, and the columns whose type moved.
//
// The decision is taken here, without going elsewhere: keep what the run chose, or pick another
// transformer and apply it. Either way the change is marked reviewed, with an optional note.
export default function MappingChangesCard(props: Props): ReactElement {
  const { jobId } = props;
  const { account } = useAccount();
  const accountId = account?.id ?? '';

  const { data, isLoading } = useQuery(
    JobService.method.getPendingMappingChanges,
    { accountId, jobId },
    { enabled: !!accountId && !!jobId }
  );
  const { handler, isLoading: isTransformersLoading } =
    useGetTransformersHandler(accountId);
  // The source connection is what the preview reads the column from.
  const { data: jobData } = useQuery(
    JobService.method.getJob,
    { id: jobId },
    { enabled: !!jobId }
  );
  const sourceConnectionId = getConnectionIdFromSource(jobData?.job?.source);
  const { mutateAsync: reviewChanges } = useMutation(
    JobService.method.reviewMappingChanges
  );
  const { mutateAsync: applyChanges } = useMutation(
    JobService.method.applyMappingChanges
  );
  const queryClient = useQueryClient();

  // Every pending-changes query, this job's and the account's bell alike, and the job itself,
  // whose mappings an apply changes: the keys carry no input, so they match them all.
  async function refresh(): Promise<void> {
    await Promise.all([
      queryClient.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: JobService.method.getPendingMappingChanges,
          cardinality: undefined,
        }),
      }),
      queryClient.invalidateQueries({
        queryKey: createConnectQueryKey({
          schema: JobService.method.getJob,
          cardinality: undefined,
        }),
      }),
    ]);
  }

  const pending = useMemo(
    () => [...(data?.changes ?? [])].sort((a, b) => urgency(a) - urgency(b)),
    [data?.changes]
  );
  const [selected, setSelected] = useState<Set<string>>(new Set());
  // The transformer picked for a change. Absent means the one the run chose.
  const [chosen, setChosen] = useState<
    Record<string, JobMappingTransformerForm>
  >({});
  const [dialog, setDialog] = useState<{
    action: ReviewAction;
    changes: JobMappingChange[];
  } | null>(null);
  const [previewing, setPreviewing] = useState<JobMappingChange | null>(null);

  function transformerFor(c: JobMappingChange): JobMappingTransformerForm {
    if (chosen[c.id]) {
      return chosen[c.id];
    }
    return c.transformer
      ? convertJobMappingTransformerToForm(c.transformer)
      : NO_TRANSFORMER;
  }

  function transformerName(c: JobMappingChange): string {
    const transformer = transformerFor(c);
    if (!transformer.config.case) {
      return '—';
    }
    return getTransformerFromField(handler, transformer).name;
  }

  function toggle(id: string): void {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }

  async function review(
    changes: JobMappingChange[],
    note?: string
  ): Promise<void> {
    try {
      const resp = await reviewChanges({
        accountId,
        jobId,
        changeIds: changes.map((c) => c.id),
        note,
      });
      toast.success(
        `${resp.changeIds.length} change${resp.changeIds.length === 1 ? '' : 's'} reviewed`
      );
      setSelected(new Set());
      await refresh();
    } catch (error) {
      toast.error('Unable to mark these changes reviewed', {
        description: getErrorMessage(error),
      });
      // Rethrown so the dialog stays open and keeps the note.
      throw error;
    }
  }

  async function apply(
    changes: JobMappingChange[],
    note?: string
  ): Promise<void> {
    try {
      const resp = await applyChanges({
        accountId,
        jobId,
        mappings: changes.map((c) =>
          create(JobMappingSchema, {
            schema: c.column?.schema,
            table: c.column?.table,
            column: c.column?.column,
            transformer:
              convertJobMappingTransformerFormToJobMappingTransformer(
                transformerFor(c)
              ),
          })
        ),
        changeIds: changes.map((c) => c.id),
        note,
      });
      toast.success(
        `${resp.mappings.length} column${resp.mappings.length === 1 ? '' : 's'} mapped from the next run`
      );
      setSelected(new Set());
      setChosen((prev) => {
        const next = { ...prev };
        for (const c of changes) {
          delete next[c.id];
        }
        return next;
      });
      await refresh();
    } catch (error) {
      toast.error('Unable to apply these transformers', {
        description: getErrorMessage(error),
      });
      throw error;
    }
  }

  if (isLoading || isTransformersLoading) {
    return <Skeleton className="w-full h-48" />;
  }

  const selectedChanges = pending.filter((c) => selected.has(c.id));
  const selectedCorrectable = selectedChanges.filter(
    (c) => isCorrectable(c) && !!transformerFor(c).config.case
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>Changes to review</CardTitle>
        <CardDescription>
          What this job&apos;s runs changed in its mappings as its source
          evolved. Keep what they chose, or pick another transformer and apply
          it.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {pending.length === 0 ? (
          <div className="flex flex-row items-center gap-2 text-sm rounded-xl p-4 bg-green-100 dark:bg-green-900 text-green-900 dark:text-green-200">
            <CheckCircledIcon />
            Nothing waiting: every change the runs made has been reviewed.
          </div>
        ) : (
          <>
            <div className="flex flex-row items-center gap-2">
              <Button
                type="button"
                disabled={selectedCorrectable.length === 0}
                onClick={() =>
                  setDialog({ action: 'apply', changes: selectedCorrectable })
                }
              >
                Apply selection… ({selectedCorrectable.length})
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={selectedChanges.length === 0}
                onClick={() =>
                  setDialog({ action: 'review', changes: selectedChanges })
                }
              >
                Mark reviewed… ({selectedChanges.length})
              </Button>
            </div>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-8">
                    <input
                      type="checkbox"
                      aria-label="Select all"
                      checked={selected.size === pending.length}
                      onChange={(e) =>
                        setSelected(
                          e.target.checked
                            ? new Set(pending.map((c) => c.id))
                            : new Set()
                        )
                      }
                    />
                  </TableHead>
                  <TableHead>Column</TableHead>
                  <TableHead>Change</TableHead>
                  <TableHead>Transformer</TableHead>
                  <TableHead>When</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pending.map((c) => {
                  const transformer = transformerFor(c);
                  return (
                    <TableRow key={c.id}>
                      <TableCell>
                        <input
                          type="checkbox"
                          aria-label={`Select ${columnName(c)}`}
                          checked={selected.has(c.id)}
                          onChange={() => toggle(c.id)}
                        />
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {columnName(c)}
                        <div className="text-muted-foreground">
                          {c.dataType}
                        </div>
                      </TableCell>
                      <TableCell className="flex flex-col gap-1 items-start">
                        <Badge variant="outline">{changeLabel(c)}</Badge>
                        {c.piiCategory && (
                          <Badge
                            variant={
                              c.kind === JobMappingChangeKind.ADDED &&
                              isPassthrough(c)
                                ? 'destructive'
                                : 'secondary'
                            }
                          >
                            Personal data: {c.piiCategory}
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        {isCorrectable(c) ? (
                          <div className="flex flex-row items-center gap-2">
                            <TransformerSelect
                              getTransformers={() =>
                                getFilterdTransformersByType(
                                  handler,
                                  dbDataTypeToTransformerDataType(c.dataType)
                                )
                              }
                              value={transformer}
                              buttonText={getTransformerSelectButtonText(
                                getTransformerFromField(handler, transformer),
                                'Choose a transformer'
                              )}
                              onSelect={(value) =>
                                setChosen((prev) => ({
                                  ...prev,
                                  [c.id]: value,
                                }))
                              }
                              disabled={false}
                            />
                            {/* The options of the transformer, as on the source page. */}
                            <EditTransformerOptions
                              transformer={getTransformerFromField(
                                handler,
                                transformer
                              )}
                              value={transformer}
                              onSubmit={(value) =>
                                setChosen((prev) => ({
                                  ...prev,
                                  [c.id]: value,
                                }))
                              }
                              disabled={isInvalidTransformer(
                                getTransformerFromField(handler, transformer)
                              )}
                            />
                            <Button
                              type="button"
                              size="sm"
                              variant="ghost"
                              aria-label={`Preview ${columnName(c)}`}
                              title="Preview the values, and what the transformer makes of them"
                              disabled={!sourceConnectionId}
                              onClick={() => setPreviewing(c)}
                            >
                              <EyeOpenIcon />
                            </Button>
                            <Button
                              type="button"
                              size="sm"
                              disabled={!transformer.config.case}
                              onClick={() =>
                                setDialog({ action: 'apply', changes: [c] })
                              }
                            >
                              Apply
                            </Button>
                          </div>
                        ) : (
                          <span className="text-xs text-muted-foreground">
                            was {transformerName(c)}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs">
                        {c.createdAt
                          ? formatDateTime(timestampDate(c.createdAt))
                          : '—'}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </>
        )}
      </CardContent>
      {previewing && sourceConnectionId && (
        <ColumnPreviewDialog
          open
          onOpenChange={(open) => {
            if (!open) {
              setPreviewing(null);
            }
          }}
          connectionId={sourceConnectionId}
          schema={previewing.column?.schema ?? ''}
          table={previewing.column?.table ?? ''}
          column={previewing.column?.column ?? ''}
          dataType={previewing.dataType}
          transformer={previewTransformer(transformerFor(previewing))}
          transformerName={transformerName(previewing)}
        />
      )}
      <ReviewChangesDialog
        open={!!dialog}
        onOpenChange={(open) => {
          if (!open) {
            setDialog(null);
          }
        }}
        action={dialog?.action ?? 'review'}
        changes={dialog?.changes ?? []}
        transformerNameOf={transformerName}
        onConfirm={dialog?.action === 'apply' ? apply : review}
      />
    </Card>
  );
}

// The transformer the preview applies: none for a passthrough, whose output is its input.
function previewTransformer(transformer: JobMappingTransformerForm) {
  if (
    !transformer.config.case ||
    transformer.config.case === 'passthroughConfig'
  ) {
    return undefined;
  }
  return convertJobMappingTransformerFormToJobMappingTransformer(transformer)
    .config;
}
