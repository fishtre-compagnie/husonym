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
import { useGetTransformersHandler } from '@/libs/hooks/useGetTransformersHandler';
import { cn } from '@/libs/utils';
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
import { LuPanelRight } from 'react-icons/lu';
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
// Grouped by table, read like a diff. A click opens the change in the panel the source page
// uses too, to keep the run's choice or replace it; the selection confirms several at once.
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
  const groups = useMemo(() => {
    const byTable = new Map<string, JobMappingChange[]>();
    pending.forEach((c) => {
      const key = `${c.column?.schema ?? ''}.${c.column?.table ?? ''}`;
      byTable.set(key, [...(byTable.get(key) ?? []), c]);
    });
    return [...byTable.entries()];
  }, [pending]);

  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [reviewing, setReviewing] = useState<JobMappingChange[]>([]);
  const [openedId, setOpenedId] = useState<string | null>(null);

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
  // Naviguer dans l'ordre de la liste, pas dans celui de `pending` : la liste groupe par
  // table, `pending` trie par urgence. Dès qu'une table mêlait deux urgences, « suivant »
  // sautait à une autre table que celle qu'on lisait.
  const ordered = groups.flatMap(([, changes]) => changes);
  const position = ordered.findIndex((c) => c.id === openedId);
  const opened = position >= 0 ? ordered[position] : null;

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
          <div className="flex flex-col rounded-xl border overflow-hidden">
            {groups.map(([table, changes]) => (
              <div key={table} className="flex flex-col">
                <div className="flex flex-row items-center gap-3 px-4 py-2 bg-gray-50 dark:bg-gray-800 border-b">
                  <input
                    type="checkbox"
                    aria-label={`Select every change of ${table}`}
                    checked={changes.every((c) => selected.has(c.id))}
                    onChange={(e) =>
                      setSelected((prev) => {
                        const next = new Set(prev);
                        changes.forEach((c) =>
                          e.target.checked ? next.add(c.id) : next.delete(c.id)
                        );
                        return next;
                      })
                    }
                  />
                  <span className="font-mono text-xs">{table}</span>
                  <span className="text-xs text-muted-foreground">
                    {changes.length} change{changes.length === 1 ? '' : 's'}
                  </span>
                </div>
                {changes.map((c) => (
                  <ChangeRow
                    key={c.id}
                    change={c}
                    isOpened={c.id === openedId}
                    isSelected={selected.has(c.id)}
                    onSelect={() => toggle(c.id)}
                    onOpen={() => setOpenedId(c.id)}
                    transformerName={transformerName(c)}
                  />
                ))}
              </div>
            ))}
          </div>
        )}
      </CardContent>

      {selectedChanges.length > 0 && (
        <div className="sticky bottom-4 z-20 flex justify-center pointer-events-none">
          <div className="pointer-events-auto flex flex-row items-center gap-3 rounded-xl bg-gray-900 dark:bg-gray-800 text-gray-50 px-4 py-2 shadow-lg">
            <span className="text-sm font-medium">
              {selectedChanges.length} change
              {selectedChanges.length === 1 ? '' : 's'} selected
            </span>
            <span className="h-5 w-px bg-gray-600" />
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => setReviewing(selectedChanges)}
            >
              Mark reviewed…
            </Button>
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="text-gray-300 hover:text-gray-50 hover:bg-gray-700"
              onClick={() => setSelected(new Set())}
            >
              Clear
            </Button>
          </div>
        </div>
      )}

      {opened && (
        <ChangeDecisionPanel
          key={opened.id}
          change={opened}
          onClose={() => setOpenedId(null)}
          handler={handler}
          sourceConnectionId={sourceConnectionId}
          navigation={{
            position: position + 1,
            total: ordered.length,
            onPrevious:
              position > 0
                ? () => setOpenedId(ordered[position - 1].id)
                : undefined,
            onNext:
              position < ordered.length - 1
                ? () => setOpenedId(ordered[position + 1].id)
                : undefined,
          }}
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

interface RowProps {
  change: JobMappingChange;
  isOpened: boolean;
  isSelected: boolean;
  onSelect(): void;
  onOpen(): void;
  transformerName: string;
}

function ChangeRow(props: RowProps): ReactElement {
  const { change, isOpened, isSelected, onSelect, onOpen, transformerName } =
    props;
  const options = summarizeOptions(change.transformer?.config);
  const removed = change.kind === JobMappingChangeKind.REMOVED;
  const inClear =
    change.kind === JobMappingChangeKind.ADDED && isPassthrough(change);

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={onOpen}
      onKeyDown={(e) => {
        // Seulement quand la ligne elle-même a le focus : la case à cocher est dedans,
        // et Espace dessus remontait jusqu'ici, où preventDefault annulait la coche et
        // ouvrait le panneau à la place. La sélection multiple devenait inatteignable
        // au clavier. Le clic est protégé de la même façon, par stopPropagation.
        if (e.target !== e.currentTarget) {
          return;
        }
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onOpen();
        }
      }}
      className={cn(
        'flex flex-row items-center gap-3 px-4 h-16 border-b last:border-b-0 cursor-pointer text-left',
        isOpened ? 'bg-gray-100 dark:bg-gray-800' : 'hover:bg-gray-50'
      )}
    >
      <span onClick={(e) => e.stopPropagation()}>
        <input
          type="checkbox"
          aria-label={`Select ${columnName(change)}`}
          checked={isSelected}
          onChange={onSelect}
        />
      </span>
      <span
        aria-hidden
        className={cn(
          'flex items-center justify-center h-6 w-6 rounded-md text-sm font-semibold shrink-0',
          removed
            ? 'bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300'
            : change.kind === JobMappingChangeKind.ADDED
              ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300'
              : 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
        )}
      >
        {removed ? '−' : change.kind === JobMappingChangeKind.ADDED ? '+' : '~'}
      </span>
      <span className="flex flex-col w-56 shrink-0">
        <span className="font-mono text-xs truncate">
          {change.column?.column}
        </span>
        <span className="text-xs text-muted-foreground truncate">
          {changeLabel(change)}
        </span>
      </span>
      <span className="flex flex-col w-64 shrink-0">
        <span className={cn('text-sm truncate', inClear && 'text-amber-700')}>
          {removed
            ? `was ${transformerName}`
            : inClear
              ? 'Passthrough — copied in clear'
              : transformerName}
        </span>
        <span className="text-xs text-muted-foreground truncate">
          {options.length > 0
            ? options.join(' · ')
            : removed
              ? 'Mapping removed from the job'
              : ''}
        </span>
      </span>
      <span className="grow text-xs text-muted-foreground">
        {change.piiCategory && (
          <Badge variant={inClear ? 'destructive' : 'secondary'}>
            Personal data: {change.piiCategory}
          </Badge>
        )}
      </span>
      <span className="text-xs text-muted-foreground w-40 shrink-0">
        {change.createdAt
          ? formatDateTime(timestampDate(change.createdAt))
          : ''}
      </span>
      <LuPanelRight
        aria-hidden
        className="h-4 w-4 text-muted-foreground shrink-0"
      />
    </div>
  );
}
