import { getConnectionIdFromSource } from '@/app/(mgmt)/[account]/jobs/[id]/source/components/util';
import { summarizeOptions } from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/options/summary';
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
  changeLabel,
  columnName,
  isPassthrough,
  urgency,
} from '@/util/mapping-changes';
import {
  formatDateTime,
  getErrorMessage,
  getTransformerFromField,
} from '@/util/util';
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
import { CheckCircledIcon } from '@radix-ui/react-icons';
import { useQueryClient } from '@tanstack/react-query';
import { ReactElement, useMemo, useState } from 'react';
import { toast } from 'sonner';
import ChangeDecisionPanel from './ChangeDecisionPanel';
import ReviewChangesDialog from './ReviewChangesDialog';

interface Props {
  jobId: string;
}

// What the job's runs changed in its mappings and nobody has reviewed yet: the columns they
// mapped — with the transformer they chose, or in clear when none applied — the mappings they
// removed with their column, and the columns whose type moved.
//
// Each row says what the run decided, options included, without opening anything. A click opens
// the change in a panel where the transformer, its options and their effect on the column's
// values sit together, to keep the run's choice or replace it. The selection is for confirming
// several changes at once.
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
  const [reviewing, setReviewing] = useState<JobMappingChange[]>([]);
  const [opened, setOpened] = useState<JobMappingChange | null>(null);

  function transformerName(c: JobMappingChange): string {
    if (!c.transformer?.config?.config.case) {
      return '—';
    }
    return getTransformerFromField(
      handler,
      convertJobMappingTransformerToForm(c.transformer)
    ).name;
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
      // Rethrown so the dialog or the panel stays open, and keeps the note.
      throw error;
    }
  }

  async function apply(
    change: JobMappingChange,
    transformer: JobMappingTransformerForm,
    note?: string
  ): Promise<void> {
    try {
      await applyChanges({
        accountId,
        jobId,
        mappings: [
          create(JobMappingSchema, {
            schema: change.column?.schema,
            table: change.column?.table,
            column: change.column?.column,
            transformer:
              convertJobMappingTransformerFormToJobMappingTransformer(
                transformer
              ),
          }),
        ],
        changeIds: [change.id],
        note,
      });
      toast.success(`${columnName(change)} mapped from the next run`);
      await refresh();
    } catch (error) {
      toast.error('Unable to apply this transformer', {
        description: getErrorMessage(error),
      });
      throw error;
    }
  }

  if (isLoading || isTransformersLoading) {
    return <Skeleton className="w-full h-48" />;
  }

  const selectedChanges = pending.filter((c) => selected.has(c.id));

  return (
    <Card>
      <CardHeader>
        <CardTitle>Changes to review</CardTitle>
        <CardDescription>
          What this job&apos;s runs changed in its mappings as its source
          evolved. Open a change to keep what the run chose or pick another
          transformer; select several to confirm them at once.
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
                variant="outline"
                disabled={selectedChanges.length === 0}
                onClick={() => setReviewing(selectedChanges)}
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
                  const options = summarizeOptions(c.transformer?.config);
                  return (
                    <TableRow
                      key={c.id}
                      className="cursor-pointer"
                      onClick={() => setOpened(c)}
                    >
                      <TableCell onClick={(e) => e.stopPropagation()}>
                        <input
                          type="checkbox"
                          aria-label={`Select ${columnName(c)}`}
                          checked={selected.has(c.id)}
                          onChange={() => toggle(c.id)}
                        />
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {/* A button, so the change opens from the keyboard too. */}
                        <button
                          type="button"
                          className="text-left hover:underline underline-offset-2"
                          onClick={(e) => {
                            e.stopPropagation();
                            setOpened(c);
                          }}
                        >
                          {columnName(c)}
                        </button>
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
                      <TableCell className="text-xs">
                        {c.kind === JobMappingChangeKind.REMOVED ? (
                          <span className="text-muted-foreground">
                            was {transformerName(c)}
                          </span>
                        ) : isPassthrough(c) ? (
                          <span className="font-medium">
                            Passthrough — copied in clear
                          </span>
                        ) : (
                          <span>{transformerName(c)}</span>
                        )}
                        {options.length > 0 && (
                          <div className="text-muted-foreground">
                            {options.join(' · ')}
                          </div>
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
      {opened && (
        <ChangeDecisionPanel
          key={opened.id}
          change={opened}
          onClose={() => setOpened(null)}
          handler={handler}
          sourceConnectionId={sourceConnectionId}
          onReview={(change, note) => review([change], note)}
          onApply={apply}
        />
      )}
      <ReviewChangesDialog
        open={reviewing.length > 0}
        onOpenChange={(open) => {
          if (!open) {
            setReviewing([]);
          }
        }}
        changes={reviewing}
        onConfirm={review}
      />
    </Card>
  );
}
