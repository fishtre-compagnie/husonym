import { getConnectionIdFromSource } from '@/app/(mgmt)/[account]/jobs/[id]/source/components/util';
import ColumnPreviewDialog from '@/components/jobs/JobMappingTable/ColumnPreviewDialog';
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
  getTransformerFromField,
} from '@/util/util';
import { convertJobMappingTransformerToForm } from '@/yup-validations/jobs';
import { timestampDate } from '@bufbuild/protobuf/wkt';
import {
  createConnectQueryKey,
  useMutation,
  useQuery,
} from '@connectrpc/connect-query';
import {
  JobMappingChange,
  JobMappingChangeKind,
  JobService,
} from '@husonym/sdk';
import { CheckCircledIcon, EyeOpenIcon } from '@radix-ui/react-icons';
import { useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { ReactElement, useMemo, useState } from 'react';
import { toast } from 'sonner';
import {
  changeLabel,
  columnName,
  isPassthrough,
  urgency,
} from '@/util/mapping-changes';
import ReviewChangesDialog from './ReviewChangesDialog';

interface Props {
  jobId: string;
}

// What the job's runs changed in its mappings and nobody has reviewed yet: the columns they
// mapped — with the transformer they chose, or in clear when none applied — the mappings they
// removed with their column, and the columns whose type moved.
//
// Changing a mapping is done where mappings are edited, the job's source page; a change whose
// mapping somebody changed leaves this list on its own. What is left here is to confirm.
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
  const queryClient = useQueryClient();

  // Every pending-changes query, this job's and the account's bell alike: the key carries no
  // input, so it matches them all.
  async function refreshPending(): Promise<void> {
    await queryClient.invalidateQueries({
      queryKey: createConnectQueryKey({
        schema: JobService.method.getPendingMappingChanges,
        cardinality: undefined,
      }),
    });
  }

  const pending = useMemo(
    () => [...(data?.changes ?? [])].sort((a, b) => urgency(a) - urgency(b)),
    [data?.changes]
  );
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reviewing, setReviewing] = useState<JobMappingChange[]>([]);
  const [previewing, setPreviewing] = useState<JobMappingChange | null>(null);

  function transformerName(c: JobMappingChange): string {
    if (!c.transformer?.config) {
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
      await refreshPending();
    } catch (error) {
      toast.error('Unable to mark these changes reviewed', {
        description: getErrorMessage(error),
      });
      // Rethrown so the dialog stays open and keeps the note.
      throw error;
    }
  }

  if (isLoading || isTransformersLoading) {
    return <Skeleton className="w-full h-48" />;
  }

  const selectedChanges = pending.filter((c) => selected.has(c.id));
  const sourceHref = account?.name
    ? `/${account.name}/jobs/${jobId}/source`
    : undefined;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Changes to review</CardTitle>
        <CardDescription>
          What this job&apos;s runs changed in its mappings as its source
          evolved. Confirm what they chose, or change the mapping on the{' '}
          {sourceHref ? (
            <Link href={sourceHref} className="underline underline-offset-2">
              source page
            </Link>
          ) : (
            'source page'
          )}
          .
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
                  <TableHead className="w-8" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {pending.map((c) => (
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
                      <div className="text-muted-foreground">{c.dataType}</div>
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
                        transformerName(c)
                      )}
                    </TableCell>
                    <TableCell className="text-xs">
                      {c.createdAt
                        ? formatDateTime(timestampDate(c.createdAt))
                        : '—'}
                    </TableCell>
                    <TableCell>
                      {c.kind !== JobMappingChangeKind.REMOVED && (
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
                      )}
                    </TableCell>
                  </TableRow>
                ))}
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
          transformer={
            isPassthrough(previewing)
              ? undefined
              : previewing.transformer?.config
          }
          transformerName={transformerName(previewing)}
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
        onReview={review}
      />
    </Card>
  );
}
