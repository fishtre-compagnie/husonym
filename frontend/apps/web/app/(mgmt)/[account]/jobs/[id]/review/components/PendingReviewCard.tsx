import { dbDataTypeToTransformerDataType } from '@/components/jobs/SchemaTable/schema-constraint-handler';
import TransformerSelect from '@/components/jobs/SchemaTable/TransformerSelect';
import { useAccount } from '@/components/providers/account-provider';
import Spinner from '@/components/Spinner';
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
} from '@/util/util';
import {
  convertJobMappingTransformerFormToJobMappingTransformer,
  convertJobMappingTransformerToForm,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import { useMutation, useQuery } from '@connectrpc/connect-query';
import {
  JobMappingSchema,
  JobMappingTransformerSchema,
  JobService,
  PendingColumnReason,
  PendingColumnReview,
  TransformerSource,
} from '@husonym/sdk';
import { CheckCircledIcon } from '@radix-ui/react-icons';
import { ReactElement, useMemo, useState } from 'react';
import { toast } from 'sonner';
import AcceptPassthroughsDialog, {
  PassthroughTarget,
} from './AcceptPassthroughsDialog';

interface Props {
  jobId: string;
}

function columnKey(c: PendingColumnReview): string {
  return `${c.tableSchema}.${c.tableName}.${c.columnName}`;
}

// What waits in this tab comes first by how much it matters: a column that reads as personal data,
// then one whose accepted passthrough no longer covers it, then the rest.
function urgency(c: PendingColumnReview): number {
  if (c.piiCategory) {
    return 0;
  }
  if (c.reason === PendingColumnReason.CHANGED_SINCE_ACCEPTED) {
    return 1;
  }
  return 2;
}

// The columns this job's last run copied untransformed, and nobody has settled.
//
// Anonymizing comes first and is the one-click path; accepting is there, but second, behind a
// dialog. The previous place to decide — a button beside each validation warning — did the
// opposite: accepting took a click, anonymizing had no path at all, so the shortest gesture was
// the one that left the data in clear.
export default function PendingReviewCard(props: Props): ReactElement {
  const { jobId } = props;
  const { account } = useAccount();
  const accountId = account?.id ?? '';

  const {
    data,
    isLoading,
    refetch: refetchPending,
  } = useQuery(
    JobService.method.getPendingColumnReviews,
    { accountId, jobId },
    { enabled: !!accountId && !!jobId }
  );
  const { handler, isLoading: isTransformersLoading } =
    useGetTransformersHandler(accountId);
  const { mutateAsync: mapColumns } = useMutation(
    JobService.method.mapUnmappedColumns
  );
  const { mutateAsync: setColumnReview } = useMutation(
    JobService.method.setColumnReview
  );

  const pending = useMemo(
    () => [...(data?.columns ?? [])].sort((a, b) => urgency(a) - urgency(b)),
    [data?.columns]
  );

  const [selected, setSelected] = useState<Set<string>>(new Set());
  // What the person picked, per column. Absent means "the suggestion", resolved below.
  const [chosen, setChosen] = useState<
    Record<string, JobMappingTransformerForm>
  >({});
  const [isApplying, setIsApplying] = useState(false);
  const [acceptTargets, setAcceptTargets] = useState<PassthroughTarget[]>([]);

  // The transformer a column starts with: the one the detection suggests, when it reads as
  // personal data. Nothing is pre-selected for the others — a guess would look more certain than
  // the detector ever was.
  function suggestedFor(
    c: PendingColumnReview
  ): JobMappingTransformerForm | undefined {
    if (c.suggestedTransformerSource === TransformerSource.UNSPECIFIED) {
      return undefined;
    }
    const system = handler
      .getTransformers()
      .system.find((t) => t.source === c.suggestedTransformerSource);
    if (!system) {
      return undefined;
    }
    return convertJobMappingTransformerToForm(
      create(JobMappingTransformerSchema, { config: system.config })
    );
  }

  function transformerFor(
    c: PendingColumnReview
  ): JobMappingTransformerForm | undefined {
    return chosen[columnKey(c)] ?? suggestedFor(c);
  }

  function toggle(key: string): void {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }

  async function apply(columns: PendingColumnReview[]): Promise<void> {
    const mappings = columns.flatMap((c) => {
      const transformer = transformerFor(c);
      if (!transformer?.config.case) {
        return [];
      }
      return [
        create(JobMappingSchema, {
          schema: c.tableSchema,
          table: c.tableName,
          column: c.columnName,
          transformer:
            convertJobMappingTransformerFormToJobMappingTransformer(
              transformer
            ),
        }),
      ];
    });
    if (mappings.length === 0) {
      return;
    }
    setIsApplying(true);
    try {
      const resp = await mapColumns({ jobId, accountId, mappings });
      const skipped = mappings.length - resp.added.length;
      toast.success(
        `${resp.added.length} column${resp.added.length === 1 ? '' : 's'} will be anonymized from the next run`,
        {
          // The server never overwrites a mapping chosen in the meantime; say so, or the count
          // above reads as a failure.
          description:
            skipped > 0
              ? `${skipped} had been mapped meanwhile and were left as they were.`
              : undefined,
        }
      );
      setSelected(new Set());
      await refetchPending();
    } catch (error) {
      toast.error('Unable to anonymize these columns', {
        description: getErrorMessage(error),
      });
    } finally {
      setIsApplying(false);
    }
  }

  async function accept(
    targets: PassthroughTarget[],
    note?: string
  ): Promise<void> {
    try {
      for (const t of targets) {
        await setColumnReview({
          jobId,
          accountId,
          tableSchema: t.schema,
          tableName: t.table,
          columnName: t.column,
          note,
        });
      }
      toast.success(
        `${targets.length} passthrough${targets.length === 1 ? '' : 's'} accepted`
      );
      setSelected(new Set());
      await refetchPending();
    } catch (error) {
      toast.error('Unable to accept these passthroughs', {
        description: getErrorMessage(error),
      });
      // Rethrown so the dialog stays open and keeps the note.
      throw error;
    }
  }

  if (isLoading || isTransformersLoading) {
    return <Skeleton className="w-full h-48" />;
  }

  const selectedColumns = pending.filter((c) => selected.has(columnKey(c)));
  const selectedApplicable = selectedColumns.filter(
    (c) => !!transformerFor(c)?.config.case
  );

  return (
    <Card>
      <CardHeader>
        <CardTitle>Columns to review</CardTitle>
        <CardDescription>
          Columns this job copied untransformed on its last run, because it does
          not map them. Anonymize them, or accept that they can stay as they
          are.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {pending.length === 0 ? (
          <div className="flex flex-row items-center gap-2 text-sm rounded-xl p-4 bg-green-100 dark:bg-green-900 text-green-900 dark:text-green-200">
            <CheckCircledIcon />
            Nothing waiting: every column this job copies is mapped or has been
            reviewed.
          </div>
        ) : (
          <>
            <div className="flex flex-row items-center gap-2">
              <Button
                type="button"
                disabled={selectedApplicable.length === 0 || isApplying}
                onClick={() => apply(selectedApplicable)}
              >
                {isApplying && <Spinner className="h-4 w-4 mr-2" />}
                Anonymize selection ({selectedApplicable.length})
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={selectedColumns.length === 0}
                onClick={() =>
                  setAcceptTargets(
                    selectedColumns.map((c) => ({
                      schema: c.tableSchema,
                      table: c.tableName,
                      column: c.columnName,
                    }))
                  )
                }
              >
                Accept selection…
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
                            ? new Set(pending.map(columnKey))
                            : new Set()
                        )
                      }
                    />
                  </TableHead>
                  <TableHead>Column</TableHead>
                  <TableHead>Why</TableHead>
                  <TableHead>Copied in clear since</TableHead>
                  <TableHead>Anonymize with</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pending.map((c) => {
                  const key = columnKey(c);
                  const transformer = transformerFor(c);
                  return (
                    <TableRow key={key}>
                      <TableCell>
                        <input
                          type="checkbox"
                          aria-label={`Select ${key}`}
                          checked={selected.has(key)}
                          onChange={() => toggle(key)}
                        />
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {key}
                        <div className="text-muted-foreground">
                          {c.dataType}
                        </div>
                      </TableCell>
                      <TableCell className="flex flex-col gap-1 items-start">
                        {c.piiCategory && (
                          <Badge variant="destructive">
                            Personal data: {c.piiCategory}
                          </Badge>
                        )}
                        {c.reason ===
                        PendingColumnReason.CHANGED_SINCE_ACCEPTED ? (
                          <Badge variant="outline">Changed since accepted</Badge>
                        ) : (
                          <Badge variant="outline">Never reviewed</Badge>
                        )}
                      </TableCell>
                      <TableCell className="text-xs">
                        {c.firstSeenAt
                          ? formatDateTime(timestampDate(c.firstSeenAt))
                          : '—'}
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-row items-center gap-2">
                          <TransformerSelect
                            getTransformers={() =>
                              getFilterdTransformersByType(
                                handler,
                                dbDataTypeToTransformerDataType(c.dataType)
                              )
                            }
                            value={
                              transformer ?? {
                                config: { case: '', value: {} },
                              }
                            }
                            buttonText={getTransformerSelectButtonText(
                              transformer
                                ? getTransformerFromField(handler, transformer)
                                : getTransformerFromField(handler, {
                                    config: { case: '', value: {} },
                                  }),
                              'Choose a transformer'
                            )}
                            onSelect={(value) =>
                              setChosen((prev) => ({ ...prev, [key]: value }))
                            }
                            disabled={isApplying}
                          />
                          <Button
                            type="button"
                            size="sm"
                            disabled={!transformer?.config.case || isApplying}
                            onClick={() => apply([c])}
                          >
                            Anonymize
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </>
        )}
      </CardContent>
      <AcceptPassthroughsDialog
        open={acceptTargets.length > 0}
        onOpenChange={(open) => {
          if (!open) {
            setAcceptTargets([]);
          }
        }}
        targets={acceptTargets}
        onAccept={accept}
      />
    </Card>
  );
}
